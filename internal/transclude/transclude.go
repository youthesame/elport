package transclude

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	xnethtml "golang.org/x/net/html"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"github.com/youthesame/elport/internal/frontmatter"
)

type Reference struct {
	Path     string
	Start    int
	End      int
	File     string
	Fragment string
	Kind     string
	Doc      string
}

type Upload struct {
	ID         int            `json:"id"`
	RealName   string         `json:"real_name"`
	LongName   string         `json:"long_name"`
	Storage    int            `json:"storage"`
	ElportName string         `json:"elport_name,omitempty"`
	Extra      map[string]any `json:"-"`
}

var windowsReserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

var keptHTMLAttributes = map[string]bool{
	"href":    true,
	"src":     true,
	"colspan": true,
	"rowspan": true,
}

func Fold(name string) string {
	return cases.Fold().String(norm.NFC.String(name))
}

func isWindowsDrive(value string) bool {
	return len(value) >= 3 && isASCIIAlpha(value[0]) && value[1] == ':' && (value[2] == '/' || value[2] == '\\')
}

func HasScheme(value string) bool {
	if isWindowsDrive(value) || value == "" || !isASCIIAlpha(value[0]) {
		return false
	}
	for i := 1; i < len(value); i++ {
		ch := value[i]
		if ch == ':' {
			return true
		}
		if !isASCIIAlpha(ch) && (ch < '0' || ch > '9') && ch != '+' && ch != '.' && ch != '-' {
			return false
		}
	}
	return false
}

func BarePaths(text string) []string {
	refs := Extract(text)
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.Path != "" && !strings.HasPrefix(r.Path, "#") && !strings.Contains(r.Path, "?") && !HasScheme(r.Path) {
			out = append(out, r.Path)
		}
	}
	return out
}

func hasHiddenPart(parts []string) bool {
	for _, p := range parts {
		if strings.HasPrefix(p, ".") && p != "." && p != ".." {
			return true
		}
	}
	return false
}

func splitRawParts(p string) []string {
	vol := filepath.VolumeName(p)
	rest := strings.TrimPrefix(p, vol)
	raw := strings.FieldsFunc(rest, func(r rune) bool { return r == '/' || r == '\\' })
	if vol != "" {
		return append([]string{vol}, raw...)
	}
	return raw
}

func splitPathParts(p string) []string {
	return splitRawParts(filepath.Clean(p))
}

func resolveJoinedNonStrict(base, rel string) string {
	var cur string
	var rest string
	if filepath.IsAbs(rel) {
		vol := filepath.VolumeName(rel)
		cur = vol + string(filepath.Separator)
		rest = strings.TrimPrefix(rel, vol)
	} else {
		cur = ResolveNonStrict(base)
		rest = rel
	}
	segs := strings.FieldsFunc(rest, func(r rune) bool { return r == '/' || r == '\\' })
	for _, seg := range segs {
		if seg == "" || seg == "." {
			continue
		}
		if seg == ".." {
			if resolved, err := filepath.EvalSymlinks(cur); err == nil {
				cur = resolved
			}
			cur = filepath.Dir(cur)
			continue
		}
		cur = filepath.Join(cur, seg)
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			cur = resolved
		}
	}
	return cur
}

func ResolveNonStrict(p string) string {
	if filepath.IsAbs(p) {
		return resolveJoinedNonStrict("", p)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return filepath.Clean(p)
	}
	return resolveJoinedNonStrict(cwd, p)
}

func hiddenAsWritten(docDir, raw string) bool {
	if !filepath.IsAbs(raw) {
		parts := strings.FieldsFunc(raw, func(r rune) bool { return r == '/' || r == '\\' })
		return hasHiddenPart(parts)
	}
	rawParts := splitRawParts(raw)
	below := func(base string) []string {
		baseParts := splitPathParts(base)
		shared := 0
		for shared < len(rawParts) && shared < len(baseParts) && rawParts[shared] == baseParts[shared] {
			shared++
		}
		return rawParts[shared:]
	}
	absDoc, err := filepath.Abs(docDir)
	if err != nil {
		absDoc = docDir
	}
	resDoc := ResolveNonStrict(docDir)
	return hasHiddenPart(below(absDoc)) && hasHiddenPart(below(resDoc))
}

func ExposesHidden(docDir, raw, target string) bool {
	if hiddenAsWritten(docDir, raw) {
		return true
	}
	root := ResolveNonStrict(docDir)
	rootParts := splitPathParts(root)
	targetParts := splitPathParts(target)
	shared := 0
	for shared < len(rootParts) && shared < len(targetParts) && rootParts[shared] == targetParts[shared] {
		shared++
	}
	return hasHiddenPart(targetParts[shared:])
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func onDiskSpelling(root string, rel string, cache map[string][]string) (string, bool) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	current := root
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		entries, ok := cache[current]
		if !ok {
			dirEntries, err := os.ReadDir(current)
			if err != nil {
				return "", false
			}
			entries = make([]string, 0, len(dirEntries))
			for _, de := range dirEntries {
				entries = append(entries, de.Name())
			}
			cache[current] = entries
		}
		found := ""
		for _, e := range entries {
			if e == part {
				found = e
				break
			}
		}
		if found == "" {
			folded := Fold(part)
			candStat, err := os.Stat(filepath.Join(current, part))
			if err != nil {
				return "", false
			}
			for _, e := range entries {
				if Fold(e) == folded {
					eStat, err := os.Stat(filepath.Join(current, e))
					if err == nil && os.SameFile(eStat, candStat) {
						found = e
						break
					}
				}
			}
		}
		if found == "" {
			return "", false
		}
		out = append(out, found)
		current = filepath.Join(current, found)
	}
	return strings.Join(out, "/"), true
}

func isRelativeTo(target, root string) (string, bool) {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

func candidate(docDir, value string, ignore []IgnoreLayer, dirCache map[string][]string) (string, string, string) {
	if value == "" || strings.HasPrefix(value, "#") || HasScheme(value) {
		return "", "", ""
	}
	if strings.Contains(value, "?") {
		return "", "has a query string", ""
	}
	raw := value
	joinPath := func(r string) string {
		return resolveJoinedNonStrict(docDir, r)
	}
	p := joinPath(raw)
	fragment := ""
	if !isRegularFile(p) && strings.Contains(raw, "#") {
		before, after, _ := strings.Cut(raw, "#")
		raw = before
		fragment = "#" + after
		p = joinPath(raw)
	}
	if !isRegularFile(p) {
		return "", "no such file", ""
	}
	if ExposesHidden(docDir, raw, p) {
		return "", "hidden path", ""
	}
	root := ResolveNonStrict(docDir)
	rel, inside := isRelativeTo(p, root)
	if !inside {
		return p, "", fragment
	}
	onDisk, ok := onDiskSpelling(root, rel, dirCache)
	if !ok {
		return "", "cannot be read", ""
	}
	relPosix := filepath.ToSlash(rel)
	if Ignored(relPosix, ignore) || Ignored(onDisk, ignore) {
		return "", "", ""
	}
	return filepath.Join(root, filepath.FromSlash(onDisk)), "", fragment
}

func linkedDocument(docDir, value string) (string, string, bool, error) {
	if value == "" || strings.HasPrefix(value, "#") || strings.Contains(value, "?") || HasScheme(value) {
		return "", "", false, nil
	}
	raw, frag, hasFrag := strings.Cut(value, "#")
	target := resolveJoinedNonStrict(docDir, raw)
	if !isRegularFile(target) || ExposesHidden(docDir, raw, target) {
		return "", "", false, nil
	}
	f, err := os.Open(target)
	if err != nil {
		return "", "", false, nil
	}
	var head [6]byte
	n, _ := f.Read(head[:])
	_ = f.Close()
	if !frontmatter.HasOpener(head[:n]) {
		return "", "", false, nil
	}
	rawBytes, err := os.ReadFile(target)
	if err != nil || !utf8.Valid(rawBytes) {
		return "", "", false, nil
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(string(rawBytes), "\r\n", "\n"), "\r", "\n")
	meta, _, err := frontmatter.Parse(normalized)
	if err != nil || meta.Len() == 0 {
		return "", "", false, nil
	}
	prof, _ := meta.Values["profile"].(string)
	if !meta.Has("id") || prof == "" {
		return "", "", false, fmt.Errorf("linked document has no id or profile yet; push it first: %s", raw)
	}
	fragment := ""
	if hasFrag {
		fragment = "#" + frag
	}
	return target, fragment, true, nil
}

func filesEqual(a, b string) bool {
	ba, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	bb, err := os.ReadFile(b)
	if err != nil {
		return false
	}
	return bytes.Equal(ba, bb)
}

func Plan(text, docDir string, ignore []IgnoreLayer, warn io.Writer) ([]Reference, error) {
	dirCache := map[string][]string{}
	m := masked(text)
	for _, dest := range scanReferenceDefinitions(m) {
		if cand, _, _ := candidate(docDir, dest, ignore, dirCache); cand != "" && warn != nil {
			_, _ = fmt.Fprintf(warn, "warning: reference-style link not uploaded (elport rewrites inline links only): %s\n", dest)
		}
	}
	refs := extractWithMask(text, m)
	names := map[string]string{}
	for i := range refs {
		docPath, docFrag, isDoc, err := linkedDocument(docDir, refs[i].Path)
		if err != nil {
			return nil, err
		}
		if isDoc {
			refs[i].Doc = docPath
			refs[i].Fragment = docFrag
			continue
		}
		p, why, frag := candidate(docDir, refs[i].Path, ignore, dirCache)
		if why != "" && warn != nil {
			_, _ = fmt.Fprintf(warn, "warning: local reference not uploaded (%s): %s\n", why, refs[i].Path)
		}
		if p != "" {
			nameKey := Fold(filepath.Base(p))
			if prev, exists := names[nameKey]; exists && prev != p {
				if !filesEqual(prev, p) {
					return nil, fmt.Errorf("basename collision: %s", filepath.Base(p))
				}
				p = prev
			}
			names[nameKey] = p
			refs[i].File = p
			refs[i].Fragment = frag
		}
	}
	return refs, nil
}

func ListedFiles(entries []string, docDir string, ignore []IgnoreLayer, referenced []string) ([]string, error) {
	dirCache := map[string][]string{}
	names := map[string]string{}
	for _, f := range referenced {
		names[Fold(filepath.Base(f))] = f
	}
	seen := map[string]bool{}
	var out []string
	for _, entry := range entries {
		p, _, frag := candidate(docDir, entry, ignore, dirCache)
		if p == "" || frag != "" {
			return nil, fmt.Errorf("attachments: cannot upload %s (must be a file, not hidden or ignored)", entry)
		}
		nameKey := Fold(filepath.Base(p))
		if prev, exists := names[nameKey]; exists {
			if prev != p && !filesEqual(prev, p) {
				return nil, fmt.Errorf("basename collision: %s", filepath.Base(p))
			}
			p = prev
		} else {
			names[nameKey] = p
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out, nil
}

func ResolveLocalPaths(paths []string, docDir string, ignore []IgnoreLayer) map[string]bool {
	dirCache := map[string][]string{}
	out := map[string]bool{}
	for _, val := range paths {
		if p, _, _ := candidate(docDir, val, ignore, dirCache); p != "" {
			out[p] = true
		}
	}
	return out
}

func ReplaceSpans(text string, refs []Reference, urls map[string]string) string {
	normalized := make(map[string]string, len(urls))
	for k, v := range urls {
		normalized[ResolveNonStrict(k)] = v
	}
	var b strings.Builder
	pos := 0
	for _, r := range refs {
		if r.Start < pos {
			continue
		}
		target := r.File
		if target == "" {
			target = r.Doc
		}
		if target == "" {
			continue
		}
		replacement, ok := normalized[ResolveNonStrict(target)]
		if !ok {
			continue
		}
		b.WriteString(text[pos:r.Start])
		b.WriteString(replacement)
		b.WriteString(r.Fragment)
		pos = r.End
	}
	b.WriteString(text[pos:])
	return b.String()
}

func DownloadURL(baseURL string, upload Upload) string {
	storage := upload.Storage
	if storage == 0 {
		storage = 1
	}
	return strings.TrimRight(baseURL, "/") + "/app/download.php?f=" +
		url.QueryEscape(upload.LongName) + "&name=" +
		url.QueryEscape(upload.RealName) + "&storage=" +
		strconv.Itoa(storage)
}

func firstNonEmptyQueryParam(q url.Values, key string) string {
	for _, v := range q[key] {
		if v != "" {
			return v
		}
	}
	return ""
}

func ParseDownloadURL(value string) (string, string, string, bool) {
	u, err := url.Parse(html.UnescapeString(value))
	if err != nil {
		return "", "", "", false
	}
	q := u.Query()
	f := firstNonEmptyQueryParam(q, "f")
	name := firstNonEmptyQueryParam(q, "name")
	storage := firstNonEmptyQueryParam(q, "storage")
	if f == "" || name == "" || storage == "" {
		return "", "", "", false
	}
	return f, name, storage, true
}

func destinationSpan(text string, r Reference) byteSpan {
	start, end := r.Start, r.End
	if r.Kind == "angle" {
		start++
		end--
	}
	for start < end {
		ru, size := utf8.DecodeRuneInString(text[start:end])
		if !pyIsSpace(ru) {
			break
		}
		start += size
	}
	for end > start {
		ru, size := utf8.DecodeLastRuneInString(text[start:end])
		if !pyIsSpace(ru) {
			break
		}
		end -= size
	}
	return byteSpan{start: start, end: end}
}

func isDownloadPrecedingForbidden(s string, pos int) bool {
	if pos <= 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(s[:pos])
	if pyIsWord(r) {
		return true
	}
	switch r {
	case '/', '.', ':', '%', '-':
		return true
	default:
		return false
	}
}

func findDownloadURLSpans(maskedText, baseURL string) []byteSpan {
	rootPrefix := strings.TrimRight(baseURL, "/") + "/"
	const marker = "app/download.php?"
	var spans []byteSpan
	pos := 0
	for pos < len(maskedText) {
		idx := strings.Index(maskedText[pos:], marker)
		if idx < 0 {
			break
		}
		mStart := pos + idx
		start := mStart
		if mStart >= len(rootPrefix) && maskedText[mStart-len(rootPrefix):mStart] == rootPrefix {
			start = mStart - len(rootPrefix)
		}
		if isDownloadPrecedingForbidden(maskedText, start) {
			pos = mStart + len(marker)
			continue
		}
		end := mStart + len(marker)
		for end < len(maskedText) {
			r, size := utf8.DecodeRuneInString(maskedText[end:])
			if pyIsSpace(r) || r == '"' || r == '\'' || r == '<' || r == '>' || r == ')' {
				break
			}
			end += size
		}
		spans = append(spans, byteSpan{start: start, end: end})
		pos = end
	}
	return spans
}

func hasUnsafeMarkdownNameChar(name string) bool {
	for _, r := range name {
		if pyIsSpace(r) || r == '(' || r == ')' {
			return true
		}
	}
	return false
}

func Reverse(text string, uploads []Upload, baseURL string) (string, []Upload) {
	m := masked(text)
	refs := extractWithMask(text, m)
	bySpan := make(map[byteSpan]Reference, len(refs))
	for _, ref := range refs {
		bySpan[destinationSpan(text, ref)] = ref
	}
	byF := make(map[string]int, len(uploads))
	for i, u := range uploads {
		byF[u.LongName] = i
	}
	var b strings.Builder
	pos := 0
	var placed []Upload
	placedIdx := map[int]bool{}
	for _, sp := range findDownloadURLSpans(m, baseURL) {
		rawURL := text[sp.start:sp.end]
		u, err := url.Parse(html.UnescapeString(rawURL))
		if err != nil {
			continue
		}
		f := firstNonEmptyQueryParam(u.Query(), "f")
		if f == "" {
			continue
		}
		uIdx, ok := byF[f]
		if !ok {
			continue
		}
		dest, ok := bySpan[sp]
		if !ok {
			continue
		}
		upload := uploads[uIdx]
		name := UploadName(upload)
		if strings.Contains(name, "#") || strings.Contains(rawURL, "#") {
			continue
		}
		if dest.Kind == "markdown" && hasUnsafeMarkdownNameChar(name) {
			name = "<" + name + ">"
		} else if dest.Kind == "html" {
			name = html.EscapeString(name)
			prev := sp.start - 1
			for prev >= 0 {
				r, size := utf8.DecodeLastRuneInString(text[:prev+1])
				if !pyIsSpace(r) {
					break
				}
				prev -= size
			}
			if prev < 0 || (text[prev] != '"' && text[prev] != '\'') {
				name = `"` + name + `"`
			}
		}
		b.WriteString(text[pos:sp.start])
		b.WriteString(name)
		pos = sp.end
		if !placedIdx[uIdx] {
			placedIdx[uIdx] = true
			placed = append(placed, upload)
		}
	}
	b.WriteString(text[pos:])
	return b.String(), placed
}

func UnmatchedDownloadURLs(text string, uploads []Upload, baseURL string) []string {
	m := masked(text)
	known := make(map[string]bool, len(uploads))
	for _, u := range uploads {
		known[u.LongName] = true
	}
	var names []string
	seen := map[string]bool{}
	for _, sp := range findDownloadURLSpans(m, baseURL) {
		rawURL := text[sp.start:sp.end]
		u, err := url.Parse(html.UnescapeString(rawURL))
		if err != nil {
			continue
		}
		q := u.Query()
		f := firstNonEmptyQueryParam(q, "f")
		if f == "" || known[f] {
			continue
		}
		name := firstNonEmptyQueryParam(q, "name")
		if name == "" {
			if slash := strings.LastIndexByte(f, '/'); slash >= 0 {
				name = f[slash+1:]
			} else {
				name = f
			}
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

func Relink(text string, docLinks map[string]map[string]string) string {
	if len(docLinks) == 0 {
		return text
	}
	var b strings.Builder
	pos := 0
	for _, ref := range Extract(text) {
		if ref.Start < pos {
			continue
		}
		family := "markdown"
		if ref.Kind == "html" {
			family = "html"
		}
		byFamily := docLinks[html.UnescapeString(ref.Path)]
		written, ok := byFamily[family]
		if ok {
			b.WriteString(text[pos:ref.Start])
			b.WriteString(written)
			pos = ref.End
		}
	}
	b.WriteString(text[pos:])
	return b.String()
}

func AbsoluteLinks(text, baseURL string) string {
	root := strings.TrimRight(baseURL, "/") + "/"
	var b strings.Builder
	pos := 0
	for _, ref := range Extract(text) {
		if strings.HasPrefix(ref.Path, "experiments.php?") || strings.HasPrefix(ref.Path, "database.php?") {
			start := destinationSpan(text, ref).start
			b.WriteString(text[pos:start])
			b.WriteString(root)
			pos = start
		}
	}
	b.WriteString(text[pos:])
	return b.String()
}

func NameUploads(uploads []Upload) []Upload {
	out := make([]Upload, len(uploads))
	copy(out, uploads)
	order := make([]int, len(out))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return out[order[i]].ID < out[order[j]].ID
	})
	taken := map[string]bool{}
	for _, idx := range order {
		realName := out[idx].RealName
		if realName == "" {
			realName = "attachment"
		}
		name := SafeName(realName)
		for taken[Fold(name)] {
			ext := filepath.Ext(name)
			stem := strings.TrimSuffix(name, ext)
			name = fmt.Sprintf("%s-%d%s", stem, out[idx].ID, ext)
		}
		taken[Fold(name)] = true
		out[idx].ElportName = name
	}
	return out
}

func UploadName(upload Upload) string {
	if upload.ElportName != "" {
		return upload.ElportName
	}
	if upload.RealName == "" {
		return "attachment"
	}
	return SafeName(upload.RealName)
}

func SafeName(name string) string {
	return SafeNameForPlatform(name, runtime.GOOS == "windows")
}

func SafeNameForPlatform(name string, isWindows bool) string {
	slash := strings.ReplaceAll(name, "\\", "/")
	if isWindows && len(slash) >= 2 && isASCIIAlpha(slash[0]) && slash[1] == ':' {
		slash = slash[2:]
	}
	slash = strings.TrimRight(slash, "/")
	base := slash
	if idx := strings.LastIndexByte(slash, '/'); idx >= 0 {
		base = slash[idx+1:]
	}
	cleaned := trimPySpace(strings.TrimLeft(base, "~"))
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return "attachment"
	}
	var b strings.Builder
	for i, r := range cleaned {
		if (i == 0 && r == '#') || r == '<' || r == '>' || r == ':' || r == '?' || r == '`' || (r >= 0 && r <= 0x1f) {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}
	cleaned = b.String()
	if isWindows {
		var wb strings.Builder
		for _, r := range cleaned {
			if r == '"' || r == '|' || r == '*' {
				wb.WriteByte('_')
			} else {
				wb.WriteRune(r)
			}
		}
		cleaned = strings.TrimRight(wb.String(), ". ")
		stem := cleaned
		if dot := strings.IndexByte(stem, '.'); dot >= 0 {
			stem = stem[:dot]
		}
		if windowsReserved[strings.ToUpper(stem)] {
			cleaned = "_" + cleaned
		}
	}
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return "attachment"
	}
	return cleaned
}

func appendReadableData(out *[]string, preDepth int, data string) {
	if preDepth == 0 && trimPySpace(data) == "" && strings.Contains(data, "\n") {
		if len(*out) > 0 && strings.HasSuffix((*out)[len(*out)-1], "\n") {
			data = ""
		} else {
			data = "\n"
		}
	}
	*out = append(*out, data)
}

func emitReadableText(out *[]string, preDepth int, raw string) {
	pos := 0
	for pos < len(raw) {
		amp := strings.IndexByte(raw[pos:], '&')
		if amp < 0 {
			appendReadableData(out, preDepth, raw[pos:])
			return
		}
		start := pos + amp
		end := start + 1
		if end < len(raw) && raw[end] == '#' {
			end++
			if end < len(raw) && (raw[end] == 'x' || raw[end] == 'X') {
				end++
				dStart := end
				for end < len(raw) && ((raw[end] >= '0' && raw[end] <= '9') || (raw[end] >= 'a' && raw[end] <= 'f') || (raw[end] >= 'A' && raw[end] <= 'F')) {
					end++
				}
				if end > dStart && end < len(raw) && raw[end] == ';' {
					if start > pos {
						appendReadableData(out, preDepth, raw[pos:start])
					}
					*out = append(*out, raw[start:end+1])
					pos = end + 1
					continue
				}
			} else {
				dStart := end
				for end < len(raw) && raw[end] >= '0' && raw[end] <= '9' {
					end++
				}
				if end > dStart && end < len(raw) && raw[end] == ';' {
					if start > pos {
						appendReadableData(out, preDepth, raw[pos:start])
					}
					*out = append(*out, raw[start:end+1])
					pos = end + 1
					continue
				}
			}
		} else {
			nStart := end
			for end < len(raw) && (isASCIIAlpha(raw[end]) || (raw[end] >= '0' && raw[end] <= '9')) {
				end++
			}
			if end > nStart && end < len(raw) && raw[end] == ';' {
				if start > pos {
					appendReadableData(out, preDepth, raw[pos:start])
				}
				*out = append(*out, raw[start:end+1])
				pos = end + 1
				continue
			}
		}
		appendReadableData(out, preDepth, raw[pos:start+1])
		pos = start + 1
	}
}

func ReadableHTML(text string) string {
	z := xnethtml.NewTokenizer(strings.NewReader(text))
	var out []string
	preDepth := 0
	for {
		tt := z.Next()
		if tt == xnethtml.ErrorToken {
			break
		}
		switch tt {
		case xnethtml.StartTagToken, xnethtml.SelfClosingTagToken:
			nameBytes, hasAttr := z.TagName()
			tag := strings.ToLower(string(nameBytes))
			if tag == "pre" {
				preDepth++
			}
			var kept strings.Builder
			for hasAttr {
				var kBytes, vBytes []byte
				kBytes, vBytes, hasAttr = z.TagAttr()
				k := strings.ToLower(string(kBytes))
				v := string(vBytes)
				if keptHTMLAttributes[k] && v != "" {
					fmt.Fprintf(&kept, ` %s="%s"`, k, html.EscapeString(v))
				}
			}
			if tag != "span" {
				out = append(out, "<"+tag+kept.String()+">")
			}
		case xnethtml.EndTagToken:
			nameBytes, _ := z.TagName()
			tag := strings.ToLower(string(nameBytes))
			if tag == "pre" && preDepth > 0 {
				preDepth--
			}
			if tag != "span" {
				out = append(out, "</"+tag+">")
			}
		case xnethtml.TextToken:
			emitReadableText(&out, preDepth, string(z.Raw()))
		case xnethtml.CommentToken, xnethtml.DoctypeToken:
		}
	}
	return strings.Join(out, "")
}
