package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/youthesame/elport/internal/config"
	"github.com/youthesame/elport/internal/frontmatter"
	"github.com/youthesame/elport/internal/fsx"
	"github.com/youthesame/elport/internal/state"
	"github.com/youthesame/elport/internal/transclude"
)

type sidecarWrite struct {
	name    string
	content string
}

func rejectExistingSidecars(docDir string, writes []sidecarWrite) error {
	root, err := fsx.OpenRoot(docDir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	for _, w := range writes {
		info, err := root.Lstat(w.name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to overwrite symlink at sidecar path: %s", w.name)
		}
		existing, err := root.ReadFile(w.name)
		if err != nil {
			return err
		}
		folded := bytes.ReplaceAll(existing, []byte("\r\n"), []byte("\n"))
		if !bytes.Equal(folded, []byte(w.content)) {
			return fmt.Errorf("refusing to overwrite existing file at sidecar path: %s; if an earlier conflict left it, delete it and retry", w.name)
		}
	}
	return nil
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for i := range len(s) {
		ch := s[i]
		ok := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') ||
			ch == '_' || ch == '@' || ch == '%' || ch == '+' || ch == '=' || ch == ':' || ch == ',' || ch == '.' || ch == '/' || ch == '-'
		if !ok {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func uploadKey(u transclude.Upload) string {
	return fmt.Sprintf("%d|%s|%s|%d|%s", u.ID, u.RealName, u.LongName, u.Storage, u.ElportName)
}

func raiseConflict(
	ctx context.Context,
	docPath string,
	remoteBody RemoteBody,
	saved map[string]any,
	uploads []transclude.Upload,
	remote *Remote,
	message string,
	stderr io.Writer,
) error {
	revUploads := ReversibleUploads(uploads)
	written := savedDocLinks(saved)
	remoteSource, remoteUsed := transclude.Reverse(transclude.Relink(string(remoteBody), written), revUploads, remote.BaseURL)
	baseSource := strField(saved, "local_base")
	_, baseUsed := transclude.Reverse(transclude.Relink(strField(saved, "remote_base"), written), revUploads, remote.BaseURL)

	remoteUsedSet := map[string]bool{}
	var used []transclude.Upload
	usedSet := map[string]bool{}
	for _, u := range remoteUsed {
		k := uploadKey(u)
		remoteUsedSet[k] = true
		if !usedSet[k] {
			usedSet[k] = true
			used = append(used, u)
		}
	}
	var baseOnly []transclude.Upload
	for _, u := range baseUsed {
		k := uploadKey(u)
		if !usedSet[k] {
			usedSet[k] = true
			used = append(used, u)
		}
		if !remoteUsedSet[k] {
			baseOnly = append(baseOnly, u)
		}
	}

	sidecar := SidecarPath(docPath)
	baseSidecar := BaseSidecarPath(docPath)
	if err := rejectSidecarAttachmentCollisions(used, []string{sidecar, baseSidecar}); err != nil {
		return err
	}
	suffixed := make([]suffixedUpload, 0, len(remoteUsed)+len(baseOnly))
	for _, u := range remoteUsed {
		suffixed = append(suffixed, suffixedUpload{upload: u, suffix: ".remote"})
	}
	for _, u := range baseOnly {
		suffixed = append(suffixed, suffixedUpload{upload: u, suffix: ".base"})
	}
	if err := rejectAttachmentConflictNameCollisions(used, suffixed); err != nil {
		return err
	}

	raw, err := os.ReadFile(docPath)
	if err != nil {
		return err
	}
	if !utf8.Valid(raw) {
		return fmt.Errorf("document is not valid UTF-8: %s", docPath)
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\r", "\n")
	meta, _, err := frontmatter.Parse(normalized)
	if err != nil {
		return err
	}

	remoteSidecarContent := frontmatter.Render(meta, remoteSource)
	baseSidecarContent := frontmatter.Render(meta, baseSource)
	docDir := filepath.Dir(docPath)
	if err := rejectExistingSidecars(docDir, []sidecarWrite{
		{name: filepath.Base(sidecar), content: remoteSidecarContent},
		{name: filepath.Base(baseSidecar), content: baseSidecarContent},
	}); err != nil {
		return err
	}

	if _, err := PlaceAttachments(ctx, docDir, remote, remoteUsed, ".remote", stderr); err != nil {
		return err
	}
	if _, err := PlaceAttachments(ctx, docDir, remote, baseOnly, ".base", stderr); err != nil {
		return err
	}
	if err := AtomicWriteDoc(sidecar, remoteSidecarContent); err != nil {
		return err
	}
	if err := AtomicWriteDoc(baseSidecar, baseSidecarContent); err != nil {
		return err
	}
	if stderr != nil {
		_, _ = fmt.Fprintf(stderr, "git merge-file -- %s %s %s\n", shellQuote(docPath), shellQuote(baseSidecar), shellQuote(sidecar))
	}

	updated := make(map[string]any, len(saved)+2)
	for k, v := range saved {
		updated[k] = v
	}
	updated["pending_remote"] = string(remoteBody)
	remembered := rememberRemotePaths(saved, strField(saved, "pending_remote"))
	if len(remembered) > 0 {
		updated["remote_paths"] = remembered
	}
	if err := state.Save(remote.BaseURL, remote.Entity, strconv.Itoa(remote.ID), updated); err != nil {
		return err
	}
	return errors.New(message)
}

func promotePendingRemote(baseURL, entity string, eid int, hasID bool, remoteSource string) error {
	if !hasID || baseURL == "" {
		return nil
	}
	unlockState, err := state.Lock(baseURL, entity, strconv.Itoa(eid))
	if err != nil {
		return err
	}
	defer unlockState()
	saved, err := state.Load(baseURL, entity, strconv.Itoa(eid))
	if err != nil || saved == nil {
		return err
	}
	if !hasKey(saved, "pending_remote") {
		return nil
	}
	updated := make(map[string]any, len(saved)+1)
	for k, v := range saved {
		updated[k] = v
	}
	updated["remote_base"] = strField(saved, "pending_remote")
	updated["local_base"] = remoteSource
	delete(updated, "pending_remote")
	delete(updated, "pending_create")
	delete(updated, "body_sent")
	remembered := rememberRemotePaths(saved, strField(saved, "remote_base"))
	if len(remembered) > 0 {
		updated["remote_paths"] = remembered
	}
	return state.Save(baseURL, entity, strconv.Itoa(eid), updated)
}

func gitPath() string {
	exe := "git"
	if runtime.GOOS == "windows" {
		exe = "git.exe"
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, exe)
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
			continue
		}
		return candidate
	}
	return ""
}

func readNormalizedUTF8(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(raw) {
		return "", fmt.Errorf("file is not valid UTF-8: %s", path)
	}
	return strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\r", "\n"), nil
}

func removeSidecars(docPath, basePath, remotePath string) error {
	root, err := fsx.OpenRoot(filepath.Dir(docPath))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := root.Remove(filepath.Base(basePath)); err != nil {
		return err
	}
	return root.Remove(filepath.Base(remotePath))
}

func Merge(ctx context.Context, docPath string, cfg *config.Data, profile string, resolved bool, stdout io.Writer) error {
	doc, docErr := ReadDocument(docPath, cfg)
	if docErr == nil && doc.HasID {
		if _, baseURL, _, berr := config.BaseTarget(cfg, profile, metaProfile(doc.Meta)); berr == nil {
			if saved, serr := state.Load(baseURL, doc.Entity, strconv.Itoa(doc.ID)); serr == nil && saved != nil {
				if _, hasPP := PendingPublish(saved); IsRegisteredHTML(saved) || hasPP {
					return fmt.Errorf("%s/%d is published in html format; merge is not used for HTML documents (inspect changes with 'elport diff' and re-run 'elport push --confirm-remote <hash>')", doc.Entity, doc.ID)
				}
			}
		}
	}
	basePath := BaseSidecarPath(docPath)
	remotePath := SidecarPath(docPath)
	_, baseErr := os.Stat(basePath)
	_, remoteErr := os.Stat(remotePath)
	if baseErr != nil || remoteErr != nil {
		return fmt.Errorf("no conflict to merge; run push/pull first (expected %s and %s)", basePath, remotePath)
	}
	if docErr != nil {
		return docErr
	}
	baseURL := ""
	if doc.HasID {
		var err error
		_, baseURL, _, err = config.BaseTarget(cfg, profile, metaProfile(doc.Meta))
		if err != nil {
			return err
		}
	}
	remoteText, err := readNormalizedUTF8(remotePath)
	if err != nil {
		return err
	}
	_, remoteSource, err := frontmatter.Parse(remoteText)
	if err != nil {
		return err
	}
	if resolved {
		if err := promotePendingRemote(baseURL, doc.Entity, doc.ID, doc.HasID, remoteSource); err != nil {
			return err
		}
		if err := removeSidecars(docPath, basePath, remotePath); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "marked resolved: %s; run 'elport push'\n", docPath)
		return nil
	}

	docDir := filepath.Dir(docPath)
	pendingSet := map[string]bool{}
	for _, ref := range transclude.Extract(remoteText) {
		if strings.Contains(ref.Path, "/") {
			continue
		}
		candidate := ref.Path + ".remote"
		if info, err := os.Stat(filepath.Join(docDir, candidate)); err == nil && info.Mode().IsRegular() {
			pendingSet[candidate] = true
		}
	}
	if len(pendingSet) > 0 {
		pending := make([]string, 0, len(pendingSet))
		for p := range pendingSet {
			pending = append(pending, p)
		}
		sort.Strings(pending)
		return fmt.Errorf("attachment conflicts remain: %s; resolve and delete them, then run 'elport merge' again", strings.Join(pending, ", "))
	}

	git := gitPath()
	if git == "" {
		return fmt.Errorf("git is not installed; merge %s and %s by hand, then run 'elport merge --resolved'", basePath, remotePath)
	}

	for _, p := range []string{docPath, basePath, remotePath} {
		norm, err := readNormalizedUTF8(p)
		if err != nil {
			return err
		}
		if err := AtomicWriteDoc(p, norm); err != nil {
			return err
		}
	}

	cmd := exec.CommandContext(ctx, git, "merge-file", "--", docPath, basePath, remotePath)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return runErr
		}
	}
	if exitCode < 0 || exitCode >= 128 {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = "git merge-file failed"
		}
		return errors.New(msg)
	}
	if err := promotePendingRemote(baseURL, doc.Entity, doc.ID, doc.HasID, remoteSource); err != nil {
		return err
	}
	if err := removeSidecars(docPath, basePath, remotePath); err != nil {
		return err
	}
	if exitCode == 0 {
		_, _ = fmt.Fprintf(stdout, "merged cleanly into %s; review and run 'elport push'\n", docPath)
		return nil
	}
	return fmt.Errorf("%d conflict(s) remain in %s; resolve the markers, then run 'elport push'", exitCode, docPath)
}
