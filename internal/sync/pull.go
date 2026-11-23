package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/youthesame/elport/internal/config"
	"github.com/youthesame/elport/internal/frontmatter"
	"github.com/youthesame/elport/internal/state"
	"github.com/youthesame/elport/internal/transclude"
)

func verifyDocUnchanged(docPath string, initial []byte) error {
	current, err := os.ReadFile(docPath)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, initial) {
		return errors.New("local document changed during pull; re-run 'elport pull'")
	}
	return nil
}

func Pull(ctx context.Context, docPath string, api API, cfg *config.Data, profile string, stdout, stderr io.Writer) (string, error) {
	initialRaw, err := os.ReadFile(docPath)
	if err != nil {
		return "", err
	}
	doc, err := parseDocumentBytes(docPath, initialRaw, cfg)
	if err != nil {
		return "", err
	}
	if !doc.HasID {
		return "", errors.New("id is required")
	}
	_, baseURL, _, err := config.BaseTarget(cfg, profile, metaProfile(doc.Meta))
	if err != nil {
		return "", err
	}
	remote := &Remote{API: api, Entity: doc.Entity, ID: doc.ID, BaseURL: baseURL}
	saved, err := state.Load(baseURL, doc.Entity, strconv.Itoa(doc.ID))
	if err != nil {
		return "", err
	}
	if err := CheckWorkingCopy(saved, docPath, doc.Entity, doc.ID); err != nil {
		return "", err
	}
	remoteDoc, err := remote.Get(ctx)
	if err != nil {
		return "", err
	}
	if err := RefuseHTML(remoteDoc, doc.Entity, doc.ID); err != nil {
		return "", err
	}
	remoteBody := RemoteBody(strField(remoteDoc, "body"))
	upToDate := saved != nil && remoteBody == RemoteBody(strField(saved, "remote_base"))
	baseMeta := savedMap(saved, "meta_base")
	localDirty := saved != nil && doc.Body != LocalBody(strField(saved, "local_base"))
	uploads, err := remote.Uploads(ctx, remoteDoc)
	if err != nil {
		return "", err
	}
	nameWhatIsAside(remote, remoteDoc, uploads, stderr)

	if localDirty && upToDate {
		merged, newBase := mergeMeta(doc.Meta, baseMeta, remoteDoc)
		_, _ = fmt.Fprintln(stdout, "remote: unchanged (local differs from the last sync; nothing to pull)")
		if metaEqual(merged, doc.Meta) && mapEqual(newBase, baseMeta) {
			return UpToDate, nil
		}
		if !metaEqual(merged, doc.Meta) {
			if err := verifyDocUnchanged(docPath, initialRaw); err != nil {
				return "", err
			}
			if err := AtomicWriteDoc(docPath, frontmatter.Render(merged, string(doc.Body))); err != nil {
				return "", err
			}
			_, _ = fmt.Fprintf(stdout, "  metadata updated in %s\n", filepath.Base(docPath))
		}
		updated := make(map[string]any, len(saved)+1)
		for k, v := range saved {
			updated[k] = v
		}
		updated["meta_base"] = newBase
		if err := state.Save(baseURL, doc.Entity, strconv.Itoa(doc.ID), updated); err != nil {
			return "", err
		}
		return UpToDate, nil
	}

	for _, u := range uploads {
		if IsHiddenUpload(u) && stderr != nil {
			_, _ = fmt.Fprintf(stderr, "warning: refusing to place hidden file attachment: %s\n", transclude.UploadName(u))
		}
	}

	relinked := transclude.Relink(string(remoteBody), savedDocLinks(saved))
	sourceStr, used := transclude.Reverse(relinked, ReversibleUploads(uploads), remote.BaseURL)
	source := LocalBody(sourceStr)

	noBaseConflict := saved == nil && strings.TrimSpace(string(doc.Body)) != "" && doc.Body != source
	if localDirty {
		return "", raiseConflict(
			ctx,
			docPath,
			remoteBody,
			saved,
			uploads,
			remote,
			fmt.Sprintf("local changes conflict; merge %s", filepath.Base(SidecarPath(docPath))),
			stderr,
		)
	}

	docDir := filepath.Dir(docPath)
	if noBaseConflict {
		sidecar := SidecarPath(docPath)
		if err := rejectSidecarAttachmentCollisions(used, []string{sidecar}); err != nil {
			return "", err
		}
		suffixed := make([]suffixedUpload, len(used))
		for i, u := range used {
			suffixed[i] = suffixedUpload{upload: u, suffix: ".remote"}
		}
		if err := rejectAttachmentConflictNameCollisions(used, suffixed); err != nil {
			return "", err
		}
		if err := rejectExistingSidecars(docDir, []sidecarWrite{{name: filepath.Base(sidecar), content: string(source)}}); err != nil {
			return "", err
		}
		attConflicts, err := PlaceAttachments(ctx, docDir, remote, used, ".remote", stderr)
		if err != nil {
			return "", err
		}
		if err := AtomicWriteDoc(sidecar, string(source)); err != nil {
			return "", err
		}
		msg := fmt.Sprintf("base unavailable; remote written to %s", filepath.Base(sidecar))
		if len(attConflicts) > 0 {
			msg += "; attachment conflicts written to: " + strings.Join(attConflicts, ", ")
		}
		return "", errors.New(msg)
	}

	suffixed := make([]suffixedUpload, len(used))
	for i, u := range used {
		suffixed[i] = suffixedUpload{upload: u, suffix: ".remote"}
	}
	if err := rejectAttachmentConflictNameCollisions(used, suffixed); err != nil {
		return "", err
	}
	nameSet := map[string]bool{}
	for _, u := range used {
		nameSet[transclude.UploadName(u)] = true
	}
	names := make([]string, 0, len(nameSet))
	for n := range nameSet {
		names = append(names, n)
	}
	sort.Strings(names)
	var newFiles []string
	for _, n := range names {
		if _, err := os.Lstat(filepath.Join(docDir, n)); errors.Is(err, os.ErrNotExist) {
			newFiles = append(newFiles, n)
		}
	}

	attConflicts, err := PlaceAttachments(ctx, docDir, remote, used, ".remote", stderr)
	if err != nil {
		return "", err
	}
	if len(attConflicts) > 0 {
		return "", fmt.Errorf(
			"attachment conflicts written to: %s; the server holds other versions: replace each file with its .remote copy, then run 'elport pull' again",
			strings.Join(attConflicts, ", "),
		)
	}

	mergedMeta, newBase := mergeMeta(doc.Meta, baseMeta, remoteDoc)
	if err := verifyDocUnchanged(docPath, initialRaw); err != nil {
		return "", err
	}
	if err := AtomicWriteDoc(docPath, frontmatter.Render(mergedMeta, string(source))); err != nil {
		return "", err
	}

	record := map[string]any{
		"remote_base": string(remoteBody),
		"local_base":  string(source),
		"meta_base":   newBase,
		"path":        transclude.ResolveNonStrict(docPath),
	}
	for k, v := range carriedState(saved) {
		record[k] = v
	}
	remembered := rememberRemotePaths(saved, strField(saved, "remote_base"), strField(saved, "pending_remote"))
	if len(remembered) > 0 {
		record["remote_paths"] = remembered
	}
	if err := state.Save(baseURL, doc.Entity, strconv.Itoa(doc.ID), record); err != nil {
		return "", err
	}

	title := strField(mergedMeta.Values, "title")
	if title == "" {
		title = strField(remoteDoc, "title")
	}
	if title == "" {
		title = PathStem(filepath.Base(docPath))
	}
	_, _ = fmt.Fprintf(stdout, "pulled %s/%d: %s\n", doc.Entity, doc.ID, title)
	wrote := append([]string{filepath.Base(docPath)}, newFiles...)
	_, _ = fmt.Fprintf(stdout, "  wrote %s\n", strings.Join(wrote, ", "))
	if shareURL := strField(remoteDoc, "sharelink"); shareURL != "" {
		_, _ = fmt.Fprintf(stdout, "  → %s\n", shareURL)
	}
	if len(newFiles) > 0 {
		return fmt.Sprintf("pulled (%d new attachment(s))", len(newFiles)), nil
	}
	if upToDate {
		return UpToDate, nil
	}
	return "pulled", nil
}
