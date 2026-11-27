package sync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/youthesame/elport/internal/config"
	"github.com/youthesame/elport/internal/frontmatter"
	"github.com/youthesame/elport/internal/fsx"
	"github.com/youthesame/elport/internal/transclude"
)

func PathStem(base string) string {
	idx := strings.LastIndexByte(base, '.')
	if idx <= 0 || idx == len(base)-1 {
		return base
	}
	allDots := true
	for i := range idx {
		if base[i] != '.' {
			allDots = false
			break
		}
	}
	if allDots {
		return base
	}
	return base[:idx]
}

func SidecarPath(docPath string) string {
	return filepath.Join(filepath.Dir(docPath), PathStem(filepath.Base(docPath))+".remote.md")
}

func BaseSidecarPath(docPath string) string {
	return filepath.Join(filepath.Dir(docPath), PathStem(filepath.Base(docPath))+".base.md")
}

func AtomicWriteDoc(docPath, content string) error {
	root, err := fsx.OpenRoot(filepath.Dir(docPath))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return root.AtomicWrite(filepath.Base(docPath), []byte(content), 0o600)
}

func WriteNewDocNoClobber(docPath, content string) error {
	root, err := fsx.OpenRoot(filepath.Dir(docPath))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return root.WriteNewNoClobber(filepath.Base(docPath), []byte(content), 0o600)
}

func rejectSidecarAttachmentCollisions(uploads []transclude.Upload, sidecars []string) error {
	names := make(map[string]bool, len(sidecars))
	for _, s := range sidecars {
		names[transclude.Fold(filepath.Base(s))] = true
	}
	for _, u := range uploads {
		name := transclude.UploadName(u)
		if names[transclude.Fold(name)] {
			return fmt.Errorf("sidecar conflicts with attachment: %s", name)
		}
	}
	return nil
}

type suffixedUpload struct {
	upload transclude.Upload
	suffix string
}

func rejectAttachmentConflictNameCollisions(planned []transclude.Upload, suffixed []suffixedUpload) error {
	plannedNames := make(map[string]bool, len(planned))
	for _, u := range planned {
		plannedNames[transclude.Fold(transclude.UploadName(u))] = true
	}
	for _, item := range suffixed {
		conflictName := transclude.UploadName(item.upload) + item.suffix
		if plannedNames[transclude.Fold(conflictName)] {
			return fmt.Errorf("attachment %s conflict-name collision", conflictName)
		}
	}
	return nil
}

func hasElportFrontmatter(data []byte) bool {
	if !frontmatter.HasOpener(data) || !utf8.Valid(data) {
		return false
	}
	_, ok := frontmatter.TextMeta(string(data))
	return ok
}

func placeConflictAttachment(root *fsx.RootDir, name string, data []byte) error {
	err := root.WriteNewNoClobber(name, data, 0o600)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrExist) {
		return err
	}
	existing, readErr := root.ReadFile(name)
	if readErr != nil {
		return readErr
	}
	if !bytes.Equal(existing, data) {
		return fmt.Errorf("refusing to overwrite existing file at attachment conflict path: %s", name)
	}
	return nil
}

func PlaceAttachments(ctx context.Context, docDir string, remote *Remote, uploads []transclude.Upload, conflictSuffix string, stderr io.Writer) ([]string, error) {
	if conflictSuffix == "" {
		conflictSuffix = ".remote"
	}
	root, err := fsx.OpenRoot(docDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	var conflicts []string
	for _, upload := range uploads {
		name := transclude.UploadName(upload)
		if IsHiddenUpload(upload) {
			if stderr != nil {
				_, _ = fmt.Fprintf(stderr, "warning: refusing to place hidden file attachment: %s\n", name)
			}
			continue
		}
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
			return nil, fmt.Errorf("unsafe attachment destination: %s", name)
		}
		if info, err := root.Lstat(name); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("unsafe attachment destination: %s", name)
			}
			digest := RemoteSHA256(upload)
			if digest != "" && info.Mode().IsRegular() {
				if localDigest, derr := SHA256File(filepath.Join(docDir, name)); derr == nil && localDigest == digest {
					continue
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}

		data, err := remote.Download(ctx, upload.ID)
		if err != nil {
			return nil, err
		}
		if hasElportFrontmatter(data) {
			return nil, fmt.Errorf("refusing to place attachment with elport front matter: %s", name)
		}

		err = root.WriteNewNoClobber(name, data, 0o666)
		if err == nil {
			continue
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		existing, readErr := root.ReadFile(name)
		if readErr != nil {
			return nil, readErr
		}
		if !bytes.Equal(existing, data) {
			conflictName := name + conflictSuffix
			if cerr := placeConflictAttachment(root, conflictName, data); cerr != nil {
				return nil, cerr
			}
			conflicts = append(conflicts, conflictName)
		}
	}
	return conflicts, nil
}

func Fetch(ctx context.Context, docPath string, api API, cfg *config.Data, profile string, stdout, stderr io.Writer) (string, error) {
	doc, err := ReadDocument(docPath, cfg)
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
	uploads, err := remote.Uploads(ctx, nil)
	if err != nil {
		return "", err
	}
	for _, u := range uploads {
		if IsHiddenUpload(u) && stderr != nil {
			_, _ = fmt.Fprintf(stderr, "warning: refusing to place hidden file attachment: %s\n", transclude.UploadName(u))
		}
	}
	attachments := ReversibleUploads(uploads)
	if err := rejectSidecarAttachmentCollisions(attachments, []string{SidecarPath(docPath), BaseSidecarPath(docPath)}); err != nil {
		return "", err
	}
	suffixed := make([]suffixedUpload, len(attachments))
	for i, u := range attachments {
		suffixed[i] = suffixedUpload{upload: u, suffix: ".remote"}
	}
	if err := rejectAttachmentConflictNameCollisions(attachments, suffixed); err != nil {
		return "", err
	}
	conflicts, err := PlaceAttachments(ctx, filepath.Dir(docPath), remote, attachments, ".remote", stderr)
	if err != nil {
		return "", err
	}
	nameSet := map[string]bool{}
	for _, u := range attachments {
		nameSet[transclude.UploadName(u)] = true
	}
	names := make([]string, 0, len(nameSet))
	for n := range nameSet {
		names = append(names, n)
	}
	sort.Strings(names)
	_, _ = fmt.Fprintf(stdout, "fetched %s/%d: %d attachment(s)\n", doc.Entity, doc.ID, len(names))
	if len(names) > 0 {
		_, _ = fmt.Fprintf(stdout, "  %s\n", strings.Join(names, ", "))
	}
	if len(conflicts) > 0 {
		_, _ = fmt.Fprintf(stdout, "  conflicts written to: %s\n", strings.Join(conflicts, ", "))
		return fmt.Sprintf("fetched %d attachment(s); conflicts: %s", len(names), strings.Join(conflicts, ", ")), nil
	}
	return fmt.Sprintf("fetched %d attachment(s)", len(names)), nil
}

func nameWhatIsAside(remote *Remote, remoteDoc map[string]any, uploads []transclude.Upload, stderr io.Writer) {
	if stderr == nil {
		return
	}
	body := strField(remoteDoc, "body")
	nameSet := map[string]bool{}
	for _, u := range uploads {
		stored := u.LongName
		if idx := strings.LastIndexByte(stored, '/'); idx >= 0 {
			stored = stored[idx+1:]
		}
		if !strings.Contains(body, stored) {
			nameSet[transclude.UploadName(u)] = true
		}
	}
	names := make([]string, 0, len(nameSet))
	for n := range nameSet {
		names = append(names, n)
	}
	sort.Strings(names)
	var parts []string
	if len(names) > 0 {
		parts = append(parts, fmt.Sprintf("%d attachment(s) (%s)", len(names), strings.Join(names, ", ")))
	}
	for _, item := range []struct {
		key  string
		word string
	}{
		{"compounds_links", "compound"},
		{"steps", "step"},
		{"comments", "comment"},
	} {
		if list, ok := remoteDoc[item.key].([]any); ok && len(list) > 0 {
			parts = append(parts, fmt.Sprintf("%d %s(s)", len(list), item.word))
		}
	}
	if len(parts) > 0 {
		_, _ = fmt.Fprintf(stderr, "not shown in the body: %s; see %s\n", strings.Join(parts, ", "), EntityURL(remote.BaseURL, remote.Entity, remote.ID))
	}
}

func listLinks(remoteDoc map[string]any, stderr io.Writer) {
	if stderr == nil {
		return
	}
	for _, dir := range []struct {
		label  string
		prefix string
	}{
		{"links to", ""},
		{"linked from", "related_"},
	} {
		for _, entity := range []string{"experiments", "items"} {
			list, ok := remoteDoc[dir.prefix+entity+"_links"].([]any)
			if !ok {
				continue
			}
			for _, raw := range list {
				link, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				_, _ = fmt.Fprintf(stderr, "%s %s/%s: %s\n", dir.label, entity, strField(link, "entityid"), strField(link, "title"))
			}
		}
	}
}

type stepEntry struct {
	hasOrder bool
	order    int
	index    int
	finished bool
	body     string
}

func parseStepOrdering(raw any) (int, bool) {
	switch x := raw.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0, false
		}
		n, err := strconv.Atoi(s)
		if err == nil {
			return n, true
		}
	default:
		if num, ok := raw.(interface{ Int64() (int64, error) }); ok {
			if n, err := num.Int64(); err == nil {
				return int(n), true
			}
		}
	}
	return 0, false
}

func listSteps(remoteDoc map[string]any, stderr io.Writer) {
	if stderr == nil {
		return
	}
	rawSteps, ok := remoteDoc["steps"].([]any)
	if !ok {
		return
	}
	var entries []stepEntry
	for idx, raw := range rawSteps {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		text := strings.Join(strings.Fields(strField(step, "body")), " ")
		if text == "" {
			continue
		}
		ord, hasOrd := parseStepOrdering(step["ordering"])
		entries = append(entries, stepEntry{
			hasOrder: hasOrd,
			order:    ord,
			index:    idx,
			finished: truthyField(step, "finished"),
			body:     text,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].hasOrder != entries[j].hasOrder {
			return entries[i].hasOrder
		}
		if entries[i].hasOrder && entries[i].order != entries[j].order {
			return entries[i].order < entries[j].order
		}
		return entries[i].index < entries[j].index
	})
	for _, s := range entries {
		mark := " "
		if s.finished {
			mark = "x"
		}
		_, _ = fmt.Fprintf(stderr, "step [%s] %s\n", mark, s.body)
	}
}

func readable(ctx context.Context, remote *Remote, remoteDoc map[string]any, stderr io.Writer) (string, []transclude.Upload, []transclude.Upload, error) {
	body := strField(remoteDoc, "body")
	if IsHTML(remoteDoc) {
		body = transclude.ReadableHTML(body)
	}
	body = transclude.AbsoluteLinks(body, remote.BaseURL)
	uploads, err := remote.Uploads(ctx, remoteDoc)
	if err != nil {
		return "", nil, nil, err
	}
	nameWhatIsAside(remote, remoteDoc, uploads, stderr)
	listLinks(remoteDoc, stderr)
	listSteps(remoteDoc, stderr)
	WarnUnmatchedAttachments(body, uploads, remote.BaseURL, stderr)
	attachments := ReversibleUploads(uploads)
	source, used := transclude.Reverse(body, attachments, remote.BaseURL)
	return source, used, attachments, nil
}

func Snapshot(ctx context.Context, api API, entity string, eid int, directory, baseURL string, stderr io.Writer) ([]string, error) {
	validID, err := frontmatter.ValidateID(eid)
	if err != nil {
		return nil, err
	}
	if entries, err := os.ReadDir(directory); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("output directory is not empty: %s", directory)
	}
	remote := &Remote{API: api, Entity: entity, ID: validID, BaseURL: baseURL}
	remoteDoc, err := remote.Get(ctx)
	if err != nil {
		return nil, err
	}
	source, _, attachments, err := readable(ctx, remote, remoteDoc, stderr)
	if err != nil {
		return nil, err
	}
	for _, u := range attachments {
		if transclude.Fold(transclude.UploadName(u)) == "report.md" {
			return nil, errors.New("an attachment is named report.md, like the body")
		}
	}
	if _, isDoc := frontmatter.TextMeta(source); isDoc {
		return nil, errors.New("the remote body starts with elport front matter")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	root, err := fsx.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	if err := root.WriteNewNoClobber("report.md", []byte(source), 0o600); err != nil {
		_ = root.Close()
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("output already exists: %s", filepath.Join(directory, "report.md"))
		}
		return nil, err
	}
	_ = root.Close()
	if _, err := PlaceAttachments(ctx, directory, remote, attachments, ".remote", stderr); err != nil {
		return nil, err
	}
	nameSet := map[string]bool{}
	for _, u := range attachments {
		nameSet[transclude.UploadName(u)] = true
	}
	names := make([]string, 0, len(nameSet))
	for n := range nameSet {
		names = append(names, n)
	}
	sort.Strings(names)
	written := make([]string, 0, 1+len(names))
	written = append(written, filepath.Join(directory, "report.md"))
	for _, n := range names {
		written = append(written, filepath.Join(directory, n))
	}
	return written, nil
}

func View(ctx context.Context, api API, entity string, eid int, baseURL, outputDir string, stdout, stderr io.Writer) error {
	if outputDir != "" {
		paths, err := Snapshot(ctx, api, entity, eid, outputDir, baseURL, stderr)
		if err != nil {
			return err
		}
		for _, p := range paths {
			if _, err := fmt.Fprintln(stdout, p); err != nil {
				return err
			}
		}
		return nil
	}
	validID, err := frontmatter.ValidateID(eid)
	if err != nil {
		return err
	}
	remote := &Remote{API: api, Entity: entity, ID: validID, BaseURL: baseURL}
	remoteDoc, err := remote.Get(ctx)
	if err != nil {
		return err
	}
	source, used, _, err := readable(ctx, remote, remoteDoc, stderr)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(stdout, source); err != nil {
		return err
	}
	nameSet := map[string]bool{}
	for _, u := range used {
		nameSet[transclude.UploadName(u)] = true
	}
	if len(nameSet) > 0 && stderr != nil {
		_, _ = fmt.Fprintf(stderr, "%d attachment(s) shown by file name; -o DIR writes them beside the body\n", len(nameSet))
	}
	return nil
}

func ViewAttachment(ctx context.Context, api API, entity string, eid int, name string, stdout io.Writer) error {
	validID, err := frontmatter.ValidateID(eid)
	if err != nil {
		return err
	}
	remote := &Remote{API: api, Entity: entity, ID: validID}
	uploads, err := remote.Uploads(ctx, nil)
	if err != nil {
		return err
	}
	attachments := ReversibleUploads(uploads)
	idx := slices.IndexFunc(attachments, func(u transclude.Upload) bool {
		return transclude.Fold(transclude.UploadName(u)) == transclude.Fold(name)
	})
	if idx < 0 {
		names := make([]string, 0, len(attachments))
		for _, u := range attachments {
			names = append(names, transclude.UploadName(u))
		}
		if len(names) == 0 {
			return fmt.Errorf("no attachment named %q on %s/%d (it has no attachments)", name, entity, validID)
		}
		sort.Strings(names)
		return fmt.Errorf("no attachment named %q on %s/%d; available: %s", name, entity, validID, strings.Join(names, ", "))
	}
	u := attachments[idx]
	data, err := remote.Download(ctx, u.ID)
	if err != nil {
		return err
	}
	if want := RemoteSHA256(u); want != "" {
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != want {
			return fmt.Errorf("attachment %s failed SHA-256 verification (server %s, downloaded %s)", transclude.UploadName(u), want, got)
		}
	}
	_, err = stdout.Write(data)
	return err
}
