package sync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pmezard/go-difflib/difflib"

	"github.com/youthesame/elport/internal/client"
	"github.com/youthesame/elport/internal/config"
	"github.com/youthesame/elport/internal/frontmatter"
	"github.com/youthesame/elport/internal/plan"
	"github.com/youthesame/elport/internal/render"
	"github.com/youthesame/elport/internal/state"
	"github.com/youthesame/elport/internal/transclude"
)

func joinBaseNames(paths []string) string {
	if len(paths) == 0 {
		return "none"
	}
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = filepath.Base(p)
	}
	return strings.Join(names, ", ")
}

func Status(ctx context.Context, docPath string, api API, cfg *config.Data, profile string, stdout, stderr io.Writer) (string, error) {
	doc, err := ReadDocument(docPath, cfg)
	if err != nil {
		return "", err
	}
	var saved map[string]any
	var remote *Remote
	if doc.HasID {
		_, baseURL, _, err := config.BaseTarget(cfg, profile, metaProfile(doc.Meta))
		if err != nil {
			return "", err
		}
		remote = &Remote{API: api, Entity: doc.Entity, ID: doc.ID, BaseURL: baseURL}
		saved, err = state.Load(baseURL, doc.Entity, strconv.Itoa(doc.ID))
		if err != nil {
			return "", err
		}
		if err := CheckWorkingCopy(saved, docPath, doc.Entity, doc.ID); err != nil {
			return "", err
		}
	}

	ignore := ignoreLayers(cfg)
	docFormat := DocFormat(doc.Meta)
	var localPlan *plan.LocalResult
	if docFormat == "html" {
		localPlan, err = plan.LocalCandidatesHTML(string(doc.Body), filepath.Dir(docPath), ignore, metaAttachments(doc.Meta), stderr)
	} else {
		localPlan, err = plan.LocalCandidates(string(doc.Body), filepath.Dir(docPath), ignore, metaAttachments(doc.Meta), stderr)
	}
	if err != nil {
		return "", err
	}

	hasBase := HasBase(saved)
	var summary []string
	if docFormat != "" {
		_, _ = fmt.Fprintf(stdout, "format: %s\n", docFormat)
	}
	if !hasBase {
		_, _ = fmt.Fprintln(stdout, "local: base unavailable (comparison unavailable)")
		summary = append(summary, "no base")
	} else if doc.Body == LocalBody(strField(saved, "local_base")) {
		_, _ = fmt.Fprintln(stdout, "local: clean")
	} else {
		_, _ = fmt.Fprintln(stdout, `local: dirty (use "elport push")`)
		summary = append(summary, "local dirty")
	}
	localFiles := localPlan.LocalFiles()
	_, _ = fmt.Fprintf(stdout, "uploads local: %s\n", joinBaseNames(localFiles))

	if remote == nil {
		_, _ = fmt.Fprintf(stdout, "uploads new: %s\n", joinBaseNames(localFiles))
		_, _ = fmt.Fprintln(stdout, "uploads reuse: none")
		_, _ = fmt.Fprintln(stdout, "remote: no id (comparison unavailable)")
		return "not pushed yet", nil
	}

	remoteDoc, err := remote.Get(ctx)
	var uploads []transclude.Upload
	if err == nil {
		uploads, err = remote.Uploads(ctx, remoteDoc)
	}
	if err != nil {
		var notFound client.NotFoundError
		if errors.As(err, &notFound) {
			_, _ = fmt.Fprintln(stdout, "remote: missing (deleted on the server?)")
			return "remote missing", nil
		}
		var connErr client.ConnectionError
		if errors.As(err, &connErr) {
			_, _ = fmt.Fprintln(stdout, "uploads reuse: unavailable (offline?)")
			_, _ = fmt.Fprintln(stdout, "remote: unavailable (offline?)")
			_, _ = fmt.Fprintln(stdout, "mode: unavailable (offline?)")
			return "remote unavailable (offline?)", nil
		}
		return "", err
	}

	WarnUnmatchedAttachments(string(doc.Body), uploads, remote.BaseURL, stderr)
	if !IsHTML(remoteDoc) && docFormat != "html" {
		WarnRawHTMLOnV6(ctx, api, string(doc.Body), stderr)
	}

	remoteBody := RemoteBody(strField(remoteDoc, "body"))
	approved, err := plan.ApproveFromLocalSources(localPlan, filepath.Dir(docPath), ignore, remoteSourcesForPlan(remoteDoc, saved, remoteBody), savedStringList(saved, "remote_paths"), stderr)
	if err != nil {
		return "", err
	}

	var reusedNames, newNames []string
	for _, ap := range approved.Files {
		name := filepath.Base(ap.Path())
		if _, matched := MatchingUpload(ap.Path(), uploads); matched {
			reusedNames = append(reusedNames, name)
		} else {
			newNames = append(newNames, name)
		}
	}
	if len(newNames) == 0 {
		_, _ = fmt.Fprintln(stdout, "uploads new: none")
	} else {
		_, _ = fmt.Fprintf(stdout, "uploads new: %s\n", strings.Join(newNames, ", "))
		summary = append(summary, fmt.Sprintf("%d new upload(s)", len(newNames)))
	}
	if len(reusedNames) == 0 {
		_, _ = fmt.Fprintln(stdout, "uploads reuse: none")
	} else {
		_, _ = fmt.Fprintf(stdout, "uploads reuse: %s\n", strings.Join(reusedNames, ", "))
	}

	isRegHTML := IsRegisteredHTML(saved)
	mode := "unknown"
	if ct, ok := intField(remoteDoc, "content_type"); ok {
		switch ct {
		case 2:
			mode = "markdown"
		case 1:
			if isRegHTML {
				mode = "html (published by elport)"
			} else {
				mode = "html (elport only reads it)"
			}
		default:
			mode = strconv.Itoa(ct)
		}
	}
	_, _ = fmt.Fprintf(stdout, "mode: %s\n", mode)

	locked := truthyField(remoteDoc, "locked")
	if locked {
		_, _ = fmt.Fprintln(stdout, "lock: locked (push refused; unlock in the Web UI)")
	}

	permChanges := PermissionChanges(EditedMeta(doc.Meta, saved), remoteDoc, nil, true)
	if len(permChanges) > 0 {
		permKeywords := map[int]string{}
		for kw, lvl := range frontmatter.PermissionLevels {
			permKeywords[lvl] = kw
		}
		parts := make([]string, 0, len(permChanges))
		differs := false
		for _, ch := range permChanges {
			if ch.Target == ch.Current {
				parts = append(parts, fmt.Sprintf("%s unchanged", ch.Field))
			} else {
				curName := permKeywords[ch.Current]
				if curName == "" {
					curName = strconv.Itoa(ch.Current)
				}
				parts = append(parts, fmt.Sprintf("%s %s→%s", ch.Field, curName, ch.Keyword))
				differs = true
			}
		}
		_, _ = fmt.Fprintf(stdout, "permissions: %s\n", strings.Join(parts, ", "))
		if differs {
			summary = append(summary, "permissions differ")
		}
	}

	_, hasPP := PendingPublish(saved)
	if !hasBase {
		_, _ = fmt.Fprintln(stdout, "remote: base unavailable (comparison unavailable)")
	} else if remoteBody == RemoteBody(strField(saved, "remote_base")) {
		_, _ = fmt.Fprintln(stdout, "remote: unchanged")
	} else if isRegHTML || IsHTML(remoteDoc) || hasPP {
		_, _ = fmt.Fprintln(stdout, `remote: changed (use "elport diff")`)
		summary = append(summary, "remote changed")
	} else {
		_, _ = fmt.Fprintln(stdout, `remote: changed (use "elport pull")`)
		summary = append(summary, "remote changed")
	}
	if hasPP {
		_, _ = fmt.Fprintln(stdout, `publish: interrupted (inspect with "elport diff" and re-run "elport push")`)
		summary = append(summary, "interrupted publish")
	}
	if isRegHTML && !IsHTML(remoteDoc) {
		summary = append(summary, "mode mismatch")
	}
	if isRegHTML {
		savedHash := ShortHash(BodySHA256(strField(saved, "remote_base")))
		if remoteBody == RemoteBody(strField(saved, "remote_base")) {
			_, _ = fmt.Fprintf(stdout, "remote sha256: %s (matches remote)\n", savedHash)
		} else {
			_, _ = fmt.Fprintf(stdout, "remote sha256: %s (remote is %s)\n", savedHash, ShortHash(BodySHA256(string(remoteBody))))
		}
	} else if IsHTML(remoteDoc) {
		_, _ = fmt.Fprintf(stdout, "remote sha256: %s\n", ShortHash(BodySHA256(string(remoteBody))))
	}
	if locked {
		summary = append(summary, "locked")
	}
	if len(summary) == 0 {
		return UpToDate, nil
	}
	return strings.Join(summary, ", "), nil
}

func normalizeRemoteDiff(text string) string {
	s := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	return strings.TrimRight(s, "\n") + "\n"
}

func splitLinesKeepEnds(s string) []string {
	if s == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := range len(s) {
		if s[i] == '\n' {
			lines = append(lines, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func Diff(ctx context.Context, docPath string, api API, cfg *config.Data, profile string, baseOnly bool, stdout, stderr io.Writer) error {
	doc, err := ReadDocument(docPath, cfg)
	if err != nil {
		return err
	}
	var saved map[string]any
	var remote *Remote
	if doc.HasID {
		_, baseURL, _, err := config.BaseTarget(cfg, profile, metaProfile(doc.Meta))
		if err != nil {
			return err
		}
		remote = &Remote{API: api, Entity: doc.Entity, ID: doc.ID, BaseURL: baseURL}
		saved, err = state.Load(baseURL, doc.Entity, strconv.Itoa(doc.ID))
		if err != nil {
			return err
		}
	}

	var other, local, fromFile string
	if baseOnly {
		if !HasBase(saved) {
			return errors.New("base unavailable; cannot show --base diff")
		}
		other = strField(saved, "local_base")
		local = string(doc.Body)
		fromFile = "base"
	} else {
		if remote == nil {
			return errors.New("id is required")
		}
		remoteDoc, err := remote.Get(ctx)
		if err != nil {
			return err
		}
		uploads, err := remote.Uploads(ctx, remoteDoc)
		if err != nil {
			return err
		}
		docFormat := DocFormat(doc.Meta)
		if IsHTML(remoteDoc) || docFormat == "html" || IsRegisteredHTML(saved) {
			rawRemote := strField(remoteDoc, "body")
			ignore := ignoreLayers(cfg)
			revertToMD := docFormat == "markdown"
			var localPlan *plan.LocalResult
			if revertToMD {
				localPlan, err = plan.LocalCandidates(string(doc.Body), filepath.Dir(docPath), ignore, metaAttachments(doc.Meta), stderr)
			} else {
				localPlan, err = plan.LocalCandidatesHTML(string(doc.Body), filepath.Dir(docPath), ignore, metaAttachments(doc.Meta), stderr)
			}
			if err != nil {
				return err
			}
			linkURLs, _, err := resolveDocumentLinks(localPlan.DocRefs(), "", stderr)
			if err != nil {
				return err
			}
			approved, err := plan.ApproveFromLocalSources(localPlan, filepath.Dir(docPath), ignore, remoteSourcesForPlan(remoteDoc, saved, RemoteBody(rawRemote)), savedStringList(saved, "remote_paths"), stderr)
			if err != nil {
				return err
			}
			urls := make(map[string]string, len(approved.Files)+len(linkURLs))
			for _, ap := range approved.Files {
				if matched, ok := MatchingUpload(ap.Path(), uploads); ok {
					urls[ap.Path()] = transclude.DownloadURL(remote.BaseURL, matched)
				} else {
					urls[ap.Path()] = "UPLOAD_PENDING:" + filepath.Base(ap.Path())
				}
			}
			for k, v := range linkURLs {
				urls[k] = v
			}
			outgoing := transclude.ReplaceSpans(string(doc.Body), approved.Refs, urls)
			var diffText string
			if revertToMD {
				fromText := normalizeRemoteDiff(rawRemote)
				if IsHTML(remoteDoc) {
					fromText = render.CanonHTML(rawRemote)
				}
				toText := normalizeRemoteDiff(outgoing)
				ud := difflib.UnifiedDiff{
					A:        splitLinesKeepEnds(fromText),
					B:        splitLinesKeepEnds(toText),
					FromFile: "remote",
					ToFile:   "local",
					Context:  3,
				}
				diffText, err = difflib.GetUnifiedDiffString(ud)
				if err != nil {
					return err
				}
			} else {
				renderedLocal, err := render.MarkdownToHTML(outgoing)
				if err != nil {
					return err
				}
				if IsHTML(remoteDoc) {
					diffText, err = CanonHTMLDiff("remote", rawRemote, "local", renderedLocal)
					if err != nil {
						return err
					}
					if diffText == "" && rawRemote != renderedLocal && stderr != nil {
						_, _ = fmt.Fprintln(stderr, "note: raw HTML differs although canonical HTML is identical")
					}
				} else {
					ud := difflib.UnifiedDiff{
						A:        splitLinesKeepEnds(normalizeRemoteDiff(rawRemote)),
						B:        splitLinesKeepEnds(render.CanonHTML(renderedLocal)),
						FromFile: "remote",
						ToFile:   "local",
						Context:  3,
					}
					diffText, err = difflib.GetUnifiedDiffString(ud)
					if err != nil {
						return err
					}
				}
			}
			if stderr != nil {
				_, _ = fmt.Fprintf(stderr, "remote sha256: %s\n", ShortHash(BodySHA256(rawRemote)))
			}
			_, err = io.WriteString(stdout, diffText)
			return err
		}
		relinked := transclude.Relink(strField(remoteDoc, "body"), savedDocLinks(saved))
		rev, _ := transclude.Reverse(relinked, uploads, remote.BaseURL)
		other = normalizeRemoteDiff(rev)
		local = normalizeRemoteDiff(string(doc.Body))
		fromFile = "remote"
	}

	ud := difflib.UnifiedDiff{
		A:        splitLinesKeepEnds(other),
		B:        splitLinesKeepEnds(local),
		FromFile: fromFile,
		ToFile:   "local",
		Context:  3,
	}
	diffText, err := difflib.GetUnifiedDiffString(ud)
	if err != nil {
		return err
	}
	if !baseOnly && diffText != "" && stderr != nil {
		_, _ = fmt.Fprintln(stderr, "note: server normalization noise may remain in this diff")
	}
	_, err = io.WriteString(stdout, diffText)
	return err
}
