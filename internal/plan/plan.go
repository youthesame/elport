package plan

import (
	"fmt"
	"io"
	"sort"

	"github.com/youthesame/elport/internal/transclude"
)

type ApprovedUploadPath struct {
	abs string
}

func (p ApprovedUploadPath) Path() string {
	return p.abs
}

func (p ApprovedUploadPath) Valid() bool {
	return p.abs != ""
}

type Result struct {
	Refs   []transclude.Reference
	Listed map[string]bool
	Files  []ApprovedUploadPath
	ByPath map[string]ApprovedUploadPath
}

type LocalResult struct {
	Refs   []transclude.Reference
	Listed map[string]bool
	Files  []string
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
		Refs:   refs,
		Listed: listed,
		Files:  files,
	}, nil
}

func Approve(body, docDir string, ignore []transclude.IgnoreLayer, listedEntries []string, remoteBodies []string, remotePaths []string, warn io.Writer) (*Result, error) {
	local, err := LocalCandidates(body, docDir, ignore, listedEntries, warn)
	if err != nil {
		return nil, err
	}
	return ApproveFromLocal(local, docDir, ignore, remoteBodies, remotePaths, warn), nil
}

func ApproveFromLocal(local *LocalResult, docDir string, ignore []transclude.IgnoreLayer, remoteBodies []string, remotePaths []string, warn io.Writer) *Result {
	refs := make([]transclude.Reference, len(local.Refs))
	copy(refs, local.Refs)

	allRemotePaths := make([]string, 0, len(remotePaths))
	allRemotePaths = append(allRemotePaths, remotePaths...)
	for _, rb := range remoteBodies {
		allRemotePaths = append(allRemotePaths, transclude.BarePaths(rb)...)
	}
	linked := transclude.ResolveLocalPaths(allRemotePaths, docDir, ignore)

	for i := range refs {
		if refs[i].File != "" && linked[refs[i].File] && !local.Listed[refs[i].File] {
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
	for p := range local.Listed {
		unionSet[p] = true
	}
	paths := make([]string, 0, len(unionSet))
	for p := range unionSet {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	files := make([]ApprovedUploadPath, 0, len(paths))
	byPath := make(map[string]ApprovedUploadPath, len(paths))
	for _, p := range paths {
		ap := ApprovedUploadPath{abs: p}
		files = append(files, ap)
		byPath[p] = ap
	}

	listedCopy := make(map[string]bool, len(local.Listed))
	for k, v := range local.Listed {
		listedCopy[k] = v
	}
	return &Result{
		Refs:   refs,
		Listed: listedCopy,
		Files:  files,
		ByPath: byPath,
	}
}
