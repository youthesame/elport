package plan

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/youthesame/elport/internal/transclude"
)

type ApprovedUploadPath struct {
	abs  string
	info os.FileInfo
}

func (p ApprovedUploadPath) Path() string {
	return p.abs
}

func (p ApprovedUploadPath) Valid() bool {
	return p.abs != "" && p.info != nil
}

func (p ApprovedUploadPath) Open() (*os.File, error) {
	if !p.Valid() {
		return nil, errors.New("refusing unapproved upload path")
	}
	root, err := os.OpenRoot(filepath.Dir(p.abs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	base := filepath.Base(p.abs)
	linfo, err := root.Lstat(base)
	if err != nil {
		return nil, err
	}
	if linfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing to upload symlink: %s", base)
	}
	f, err := root.Open(base)
	if err != nil {
		return nil, err
	}
	finfo, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !finfo.Mode().IsRegular() || !os.SameFile(finfo, p.info) || finfo.Size() != p.info.Size() || !finfo.ModTime().Equal(p.info.ModTime()) {
		_ = f.Close()
		return nil, fmt.Errorf("approved upload file changed before upload: %s", base)
	}
	return f, nil
}

type Result struct {
	Refs   []transclude.Reference
	Listed map[string]bool
	Files  []ApprovedUploadPath
	ByPath map[string]ApprovedUploadPath
}

type LocalResult struct {
	validated bool
	refs      []transclude.Reference
	listed    map[string]bool
	files     []string
}

func (l *LocalResult) LocalFiles() []string {
	if l == nil {
		return nil
	}
	out := make([]string, len(l.files))
	copy(out, l.files)
	return out
}

func (l *LocalResult) DocRefs() []transclude.Reference {
	if l == nil {
		return nil
	}
	var out []transclude.Reference
	for _, r := range l.refs {
		if r.Doc != "" {
			r.File = ""
			out = append(out, r)
		}
	}
	return out
}

func LocalCandidates(body, docDir string, ignore []transclude.IgnoreLayer, listedEntries []string, warn io.Writer) (*LocalResult, error) {
	refs, err := transclude.Plan(body, docDir, ignore, warn)
	if err != nil {
		return nil, err
	}
	referenced := make([]string, 0, len(refs))
	refSeen := map[string]bool{}
	for _, r := range refs {
		if r.File != "" && !refSeen[r.File] {
			refSeen[r.File] = true
			referenced = append(referenced, r.File)
		}
	}
	listedSlice, err := transclude.ListedFiles(listedEntries, docDir, ignore, referenced)
	if err != nil {
		return nil, err
	}
	listed := make(map[string]bool, len(listedSlice))
	unionSet := map[string]bool{}
	for _, p := range referenced {
		unionSet[p] = true
	}
	for _, p := range listedSlice {
		listed[p] = true
		unionSet[p] = true
	}
	files := make([]string, 0, len(unionSet))
	for p := range unionSet {
		files = append(files, p)
	}
	sort.Strings(files)
	return &LocalResult{
		validated: true,
		refs:      refs,
		listed:    listed,
		files:     files,
	}, nil
}

func sameDiskFile(path string, targetStats []os.FileInfo) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	for _, ts := range targetStats {
		if os.SameFile(info, ts) {
			return true
		}
	}
	return false
}

func Approve(body, docDir string, ignore []transclude.IgnoreLayer, listedEntries []string, remoteBodies []string, remotePaths []string, warn io.Writer) (*Result, error) {
	local, err := LocalCandidates(body, docDir, ignore, listedEntries, warn)
	if err != nil {
		return nil, err
	}
	return ApproveFromLocal(local, docDir, ignore, remoteBodies, remotePaths, warn)
}

func ApproveFromLocal(local *LocalResult, docDir string, ignore []transclude.IgnoreLayer, remoteBodies []string, remotePaths []string, warn io.Writer) (*Result, error) {
	if local == nil || !local.validated {
		return nil, errors.New("unvalidated local plan cannot mint approved upload paths")
	}
	refs := make([]transclude.Reference, len(local.refs))
	copy(refs, local.refs)

	allRemotePaths := make([]string, 0, len(remotePaths))
	allRemotePaths = append(allRemotePaths, remotePaths...)
	for _, rb := range remoteBodies {
		allRemotePaths = append(allRemotePaths, transclude.BarePaths(rb)...)
	}
	linked := transclude.ResolveLocalPaths(allRemotePaths, docDir, ignore)
	linkedStats := make([]os.FileInfo, 0, len(linked))
	for lp := range linked {
		if info, err := os.Stat(lp); err == nil {
			linkedStats = append(linkedStats, info)
		}
	}

	listedStats := make([]os.FileInfo, 0, len(local.listed))
	for lp, ok := range local.listed {
		if ok {
			if info, err := os.Stat(lp); err == nil {
				listedStats = append(listedStats, info)
			}
		}
	}

	for i := range refs {
		if refs[i].File == "" {
			continue
		}
		isRemoteLinked := linked[refs[i].File] || sameDiskFile(refs[i].File, linkedStats)
		isListed := local.listed[refs[i].File] || sameDiskFile(refs[i].File, listedStats)
		if isRemoteLinked && !isListed {
			if warn != nil {
				_, _ = fmt.Fprintf(warn, "warning: not uploaded, linked from the remote body (a path written on the server side; list it under attachments: only to upload it on purpose): %s\n", refs[i].Path)
			}
			refs[i].File = ""
		}
	}

	unionSet := map[string]bool{}
	for _, r := range refs {
		if r.File != "" {
			unionSet[r.File] = true
		}
	}
	for p, ok := range local.listed {
		if ok && p != "" {
			unionSet[p] = true
		}
	}
	paths := make([]string, 0, len(unionSet))
	for p := range unionSet {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	files := make([]ApprovedUploadPath, 0, len(paths))
	byPath := make(map[string]ApprovedUploadPath, len(paths))
	for _, p := range paths {
		linfo, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		if linfo.Mode()&os.ModeSymlink != 0 || !linfo.Mode().IsRegular() {
			return nil, fmt.Errorf("refusing to approve non-regular upload path: %s", filepath.Base(p))
		}
		ap := ApprovedUploadPath{abs: p, info: linfo}
		files = append(files, ap)
		byPath[p] = ap
	}

	listedCopy := make(map[string]bool, len(local.listed))
	for k, v := range local.listed {
		if v {
			listedCopy[k] = true
		}
	}
	return &Result{
		Refs:   refs,
		Listed: listedCopy,
		Files:  files,
		ByPath: byPath,
	}, nil
}
