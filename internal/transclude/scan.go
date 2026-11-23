package transclude

import (
	"html"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type byteSpan struct {
	start int
	end   int
}

type angleMatch struct {
	fullStart int
	fullEnd   int
	spanStart int
	spanEnd   int
	inner     string
}

type fenceOpen struct {
	start     int
	end       int
	prefix    string
	markerCh  byte
	markerLen int
}

func pyIsSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

func pyIsWord(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func trimPySpace(s string) string {
	return strings.TrimFunc(s, pyIsSpace)
}

func trimLeftPySpace(s string) string {
	return strings.TrimLeftFunc(s, pyIsSpace)
}

func isASCIIPunct(b byte) bool {
	return (b >= '!' && b <= '/') || (b >= ':' && b <= '@') || (b >= '[' && b <= '`') || (b >= '{' && b <= '~')
}

func blankSpan(buf []byte, start, end int) {
	for i := start; i < end; i++ {
		buf[i] = ' '
	}
}

func blankLinesSpan(buf []byte, start, end int) {
	for i := start; i < end; i++ {
		if buf[i] != '\r' && buf[i] != '\n' {
			buf[i] = ' '
		}
	}
}

func matchListItemAt(s string, pos int) int {
	if pos >= len(s) {
		return -1
	}
	ch := s[pos]
	if (ch == '-' || ch == '*' || ch == '+') && pos+1 < len(s) && (s[pos+1] == ' ' || s[pos+1] == '\t') {
		return pos + 2
	}
	if ch >= '0' && ch <= '9' {
		j := pos
		for j < len(s) && j-pos < 9 && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j < len(s) && (s[j] == '.' || s[j] == ')') && j+1 < len(s) && (s[j+1] == ' ' || s[j+1] == '\t') {
			return j + 2
		}
	}
	return -1
}

func matchFenceOpenerAt(s string, lineStart int) (fenceOpen, bool) {
	pos := lineStart
	for pos < len(s) {
		if s[pos] == ' ' || s[pos] == '\t' || s[pos] == '>' {
			pos++
			continue
		}
		if next := matchListItemAt(s, pos); next >= 0 {
			pos = next
			continue
		}
		break
	}
	if pos >= len(s) || (s[pos] != '`' && s[pos] != '~') {
		return fenceOpen{}, false
	}
	ch := s[pos]
	mStart := pos
	for pos < len(s) && s[pos] == ch {
		pos++
	}
	mLen := pos - mStart
	if mLen < 3 {
		return fenceOpen{}, false
	}
	lineEnd := strings.IndexByte(s[pos:], '\n')
	var info string
	var openEnd int
	if lineEnd < 0 {
		info = s[pos:]
		openEnd = len(s)
	} else {
		info = s[pos : pos+lineEnd]
		openEnd = pos + lineEnd + 1
	}
	if ch == '`' && strings.ContainsRune(info, '`') {
		return fenceOpen{}, false
	}
	return fenceOpen{
		start:     lineStart,
		end:       openEnd,
		prefix:    s[lineStart:mStart],
		markerCh:  ch,
		markerLen: mLen,
	}, true
}

func findNextFenceOpener(s string, from int) (fenceOpen, bool) {
	pos := from
	if pos > 0 && s[pos-1] != '\n' {
		nl := strings.IndexByte(s[pos:], '\n')
		if nl < 0 {
			return fenceOpen{}, false
		}
		pos += nl + 1
	}
	for pos <= len(s) {
		if f, ok := matchFenceOpenerAt(s, pos); ok {
			return f, true
		}
		nl := strings.IndexByte(s[pos:], '\n')
		if nl < 0 {
			break
		}
		pos += nl + 1
	}
	return fenceOpen{}, false
}

func containerPrefix(prefix string) string {
	lastEnd := 0
	pos := 0
	for pos < len(prefix) {
		if next := matchListItemAt(prefix, pos); next >= 0 {
			lastEnd = next
			pos = next
		} else {
			pos++
		}
	}
	var b strings.Builder
	p := 0
	for p < lastEnd {
		if next := matchListItemAt(prefix[:lastEnd], p); next >= 0 {
			for range next - p {
				b.WriteByte(' ')
			}
			p = next
		} else {
			b.WriteByte(prefix[p])
			p++
		}
	}
	tail := prefix[lastEnd:]
	trimSpaces := 0
	for len(tail)-trimSpaces > 0 && trimSpaces < 3 && tail[len(tail)-1-trimSpaces] == ' ' {
		trimSpaces++
	}
	b.WriteString(tail[:len(tail)-trimSpaces])
	return b.String()
}

func findFenceEnd(s string, f fenceOpen) int {
	container := containerPrefix(f.prefix)
	pos := f.end
	for pos <= len(s) {
		if strings.HasPrefix(s[pos:], container) {
			j := pos + len(container)
			sp := 0
			for j < len(s) && sp < 3 && s[j] == ' ' {
				j++
				sp++
			}
			mStart := j
			for j < len(s) && s[j] == f.markerCh {
				j++
			}
			if j-mStart >= f.markerLen {
				for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
					j++
				}
				if j == len(s) {
					return j
				}
				if s[j] == '\n' {
					return j + 1
				}
				if s[j] == '\r' && j+1 < len(s) && s[j+1] == '\n' {
					return j + 2
				}
			}
		}
		nl := strings.IndexByte(s[pos:], '\n')
		if nl < 0 {
			break
		}
		pos += nl + 1
	}
	return len(s)
}

func findNextCodeSpan(s string, from, limit int) (byteSpan, bool) {
	if limit > len(s) {
		limit = len(s)
	}
	pos := from
	for pos < limit {
		if s[pos] != '`' {
			pos++
			continue
		}
		if pos > 0 && s[pos-1] == '`' {
			pos++
			continue
		}
		runStart := pos
		for pos < limit && s[pos] == '`' {
			pos++
		}
		nTicks := pos - runStart
		cur := pos
		found := -1
		for cur < limit {
			if s[cur] == '\n' {
				k := cur + 1
				for k < limit && (s[k] == ' ' || s[k] == '\t') {
					k++
				}
				if k < limit && s[k] == '\n' {
					break
				}
			}
			if s[cur] == '`' && (cur == 0 || s[cur-1] != '`') {
				cStart := cur
				for cur < limit && s[cur] == '`' {
					cur++
				}
				if cur-cStart == nTicks {
					found = cur
					break
				}
				continue
			}
			cur++
		}
		if found >= 0 {
			return byteSpan{start: runStart, end: found}, true
		}
	}
	return byteSpan{}, false
}

func findNextComment(s string, from int) (byteSpan, bool) {
	idx := strings.Index(s[from:], "<!--")
	if idx < 0 {
		return byteSpan{}, false
	}
	start := from + idx
	endIdx := strings.Index(s[start+4:], "-->")
	if endIdx < 0 {
		return byteSpan{start: start, end: len(s)}, true
	}
	return byteSpan{start: start, end: start + 4 + endIdx + 3}, true
}

func isASCIIAlpha(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func isTagNameChar(b byte) bool {
	return isASCIIAlpha(b) || (b >= '0' && b <= '9') || b == ':' || b == '-'
}

func matchHTMLTagAt(s string, start int) (int, bool) {
	if start >= len(s) || s[start] != '<' {
		return 0, false
	}
	pos := start + 1
	if pos < len(s) && s[pos] == '/' {
		pos++
	}
	if pos >= len(s) || !isASCIIAlpha(s[pos]) {
		return 0, false
	}
	pos++
	for pos < len(s) && isTagNameChar(s[pos]) {
		pos++
	}
	if pos >= len(s) {
		return 0, false
	}
	r, _ := utf8.DecodeRuneInString(s[pos:])
	if !pyIsSpace(r) && s[pos] != '/' && s[pos] != '>' {
		return 0, false
	}
	for pos < len(s) {
		ch := s[pos]
		if ch == '>' {
			return pos + 1, true
		}
		if ch == '"' || ch == '\'' {
			q := ch
			pos++
			for pos < len(s) && s[pos] != q {
				pos++
			}
			if pos >= len(s) {
				return 0, false
			}
			pos++
			continue
		}
		pos++
	}
	return 0, false
}

func findNextHTMLTag(s string, from int) (byteSpan, bool) {
	pos := from
	for pos < len(s) {
		idx := strings.IndexByte(s[pos:], '<')
		if idx < 0 {
			return byteSpan{}, false
		}
		cand := pos + idx
		if end, ok := matchHTMLTagAt(s, cand); ok {
			return byteSpan{start: cand, end: end}, true
		}
		pos = cand + 1
	}
	return byteSpan{}, false
}

func findAllHTMLTags(s string) []byteSpan {
	var out []byteSpan
	pos := 0
	for {
		sp, ok := findNextHTMLTag(s, pos)
		if !ok {
			break
		}
		out = append(out, sp)
		pos = sp.end
	}
	return out
}

func matchOptionalTitle(s string, pos int) (int, bool) {
	if pos >= len(s) {
		return pos, false
	}
	q := s[pos]
	var closeCh byte
	switch q {
	case '"', '\'':
		closeCh = q
	case '(':
		closeCh = ')'
	default:
		return pos, false
	}
	i := pos + 1
	for i < len(s) {
		if s[i] == '\\' && i+1 < len(s) {
			i += 2
			continue
		}
		if s[i] == closeCh {
			return i + 1, true
		}
		i++
	}
	return pos, false
}

func skipPySpaces(s string, pos int) int {
	for pos < len(s) {
		r, size := utf8.DecodeRuneInString(s[pos:])
		if !pyIsSpace(r) {
			break
		}
		pos += size
	}
	return pos
}

func matchAngleDestinationAt(s string, start int) (angleMatch, bool) {
	pos := start
	if pos < len(s) && s[pos] == '!' {
		pos++
	}
	if pos >= len(s) || s[pos] != '[' {
		return angleMatch{}, false
	}
	if pos > 0 && s[pos-1] == '\\' {
		return angleMatch{}, false
	}
	closeBracket := strings.IndexByte(s[pos+1:], ']')
	if closeBracket < 0 {
		return angleMatch{}, false
	}
	pos = pos + 1 + closeBracket + 1
	if pos >= len(s) || s[pos] != '(' {
		return angleMatch{}, false
	}
	pos = skipPySpaces(s, pos+1)
	if pos >= len(s) || s[pos] != '<' {
		return angleMatch{}, false
	}
	spanStart := pos
	pos++
	innerStart := pos
	for pos < len(s) && s[pos] != '<' && s[pos] != '>' && s[pos] != '\n' {
		pos++
	}
	if pos >= len(s) || s[pos] != '>' {
		return angleMatch{}, false
	}
	innerEnd := pos
	pos++
	spanEnd := pos
	pos = skipPySpaces(s, pos)
	if pos < len(s) && (s[pos] == '"' || s[pos] == '\'' || s[pos] == '(') {
		if nextPos, ok := matchOptionalTitle(s, pos); ok {
			pos = nextPos
		}
	}
	if pos >= len(s) || s[pos] != ')' {
		return angleMatch{}, false
	}
	return angleMatch{
		fullStart: start,
		fullEnd:   pos + 1,
		spanStart: spanStart,
		spanEnd:   spanEnd,
		inner:     s[innerStart:innerEnd],
	}, true
}

func findAllAngleDestinations(s string) []angleMatch {
	var out []angleMatch
	pos := 0
	for pos < len(s) {
		if s[pos] == '!' && pos+1 < len(s) && s[pos+1] == '[' {
			if m, ok := matchAngleDestinationAt(s, pos); ok {
				out = append(out, m)
				pos = m.fullEnd
				continue
			}
		} else if s[pos] == '[' {
			if m, ok := matchAngleDestinationAt(s, pos); ok {
				out = append(out, m)
				pos = m.fullEnd
				continue
			}
		}
		pos++
	}
	return out
}

func isRawTextTagName(lower string) bool {
	switch lower {
	case "pre", "code", "script", "style", "textarea", "title":
		return true
	default:
		return false
	}
}

func maskRawTextElements(buf []byte, angles map[byteSpan]bool) {
	s := string(buf)
	pos := 0
	for pos < len(s) {
		idx := strings.IndexByte(s[pos:], '<')
		if idx < 0 {
			break
		}
		start := pos + idx
		p := start + 1
		nameStart := p
		for p < len(s) && isASCIIAlpha(s[p]) {
			p++
		}
		name := strings.ToLower(s[nameStart:p])
		if !isRawTextTagName(name) || p >= len(s) {
			pos = start + 1
			continue
		}
		r, _ := utf8.DecodeRuneInString(s[p:])
		if !pyIsSpace(r) && s[p] != '/' && s[p] != '>' {
			pos = start + 1
			continue
		}
		gt := strings.IndexByte(s[p:], '>')
		if gt < 0 {
			pos = start + 1
			continue
		}
		openEnd := p + gt + 1
		if angles[byteSpan{start: start, end: openEnd}] {
			pos = openEnd
			continue
		}
		closeEnd := len(s)
		searchPos := openEnd
		for searchPos < len(s) {
			cIdx := strings.Index(s[searchPos:], "</")
			if cIdx < 0 {
				break
			}
			cStart := searchPos + cIdx
			cnStart := cStart + 2
			if cnStart+len(name) <= len(s) && strings.EqualFold(s[cnStart:cnStart+len(name)], name) {
				after := skipPySpaces(s, cnStart+len(name))
				if after < len(s) && s[after] == '>' {
					closeEnd = after + 1
					break
				}
			}
			searchPos = cStart + 2
		}
		blankSpan(buf, start, closeEnd)
		pos = closeEnd
	}
}

func masked(text string) string {
	buf := []byte(text)
	pos := 0
	for {
		f, ok := findNextFenceOpener(text, pos)
		if !ok {
			break
		}
		end := findFenceEnd(text, f)
		blankSpan(buf, f.start, end)
		pos = end
	}
	s := string(buf)
	pos = 0
	for {
		sp, ok := findNextCodeSpan(s, pos, len(s))
		if !ok {
			break
		}
		blankSpan(buf, sp.start, sp.end)
		pos = sp.end
	}
	s = string(buf)
	pos = 0
	for {
		sp, ok := findNextComment(s, pos)
		if !ok {
			break
		}
		blankSpan(buf, sp.start, sp.end)
		pos = sp.end
	}
	s = string(buf)
	angleSet := map[byteSpan]bool{}
	for _, m := range findAllAngleDestinations(s) {
		angleSet[byteSpan{start: m.spanStart, end: m.spanEnd}] = true
	}
	maskRawTextElements(buf, angleSet)
	return string(buf)
}

func unescapeMarkdownDestination(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for pos := 0; pos < len(value); pos++ {
		if value[pos] == '\\' && pos+1 < len(value) && isASCIIPunct(value[pos+1]) {
			pos++
		}
		b.WriteByte(value[pos])
	}
	return b.String()
}

func matchMarkdownOpenerAt(s string, start int) (int, bool) {
	pos := start
	if pos < len(s) && s[pos] == '!' {
		pos++
	}
	if pos >= len(s) || s[pos] != '[' {
		return 0, false
	}
	if pos > 0 && s[pos-1] == '\\' {
		return 0, false
	}
	pos++
	for pos < len(s) && s[pos] != ']' && s[pos] != '\n' {
		pos++
	}
	if pos >= len(s) || s[pos] != ']' {
		return 0, false
	}
	pos++
	if pos >= len(s) || s[pos] != '(' {
		return 0, false
	}
	return skipPySpaces(s, pos+1), true
}

func matchMarkdownTitle(s string, pos int) (int, bool) {
	p := skipPySpaces(s, pos)
	if p == pos {
		return 0, false
	}
	afterTitle, ok := matchOptionalTitle(s, p)
	if !ok {
		return 0, false
	}
	afterTitle = skipPySpaces(s, afterTitle)
	if afterTitle >= len(s) || s[afterTitle] != ')' {
		return 0, false
	}
	return afterTitle + 1, true
}

func scanMarkdownDestinations(text, maskedText string) []Reference {
	var refs []Reference
	consumedUntil := 0
	for i := 0; i < len(maskedText); i++ {
		if i < consumedUntil {
			continue
		}
		if maskedText[i] != '!' && maskedText[i] != '[' {
			continue
		}
		start, ok := matchMarkdownOpenerAt(maskedText, i)
		if !ok {
			continue
		}
		if maskedText[i] == '!' && i+1 < len(maskedText) && maskedText[i+1] == '[' {
			i++
		}
		if start == len(maskedText) || maskedText[start] == '<' {
			continue
		}
		depth := 0
		pos := start
		for pos < len(maskedText) {
			ch := maskedText[pos]
			if ch == '\\' && pos+1 < len(maskedText) {
				pos += 2
				continue
			}
			if ch == '(' {
				depth++
				pos++
				continue
			}
			if ch == ')' {
				if depth == 0 {
					raw := text[start:pos]
					refs = append(refs, Reference{
						Path:  unescapeMarkdownDestination(raw),
						Start: start,
						End:   pos,
						Kind:  "markdown",
					})
					break
				}
				depth--
				pos++
				continue
			}
			r, size := utf8.DecodeRuneInString(maskedText[pos:])
			if ch == '<' || ch == '>' || ch == '\n' || (pyIsSpace(r) && depth == 0) {
				if titleEnd, ok := matchMarkdownTitle(maskedText, pos); ok {
					raw := text[start:pos]
					refs = append(refs, Reference{
						Path:  unescapeMarkdownDestination(raw),
						Start: start,
						End:   pos,
						Kind:  "markdown",
					})
					consumedUntil = titleEnd
				}
				break
			}
			pos += size
		}
	}
	return refs
}

type htmlAttr struct {
	name  string
	start int
	end   int
}

func scanHTMLAttributes(tag string) []htmlAttr {
	if len(tag) < 2 || tag[0] != '<' {
		return nil
	}
	pos := 1
	if pos < len(tag) && tag[pos] == '/' {
		pos++
	}
	if pos >= len(tag) || !isASCIIAlpha(tag[pos]) {
		return nil
	}
	for pos < len(tag) && isTagNameChar(tag[pos]) {
		pos++
	}
	n := len(tag)
	var attrs []htmlAttr
	for pos < n {
		for pos < n {
			r, size := utf8.DecodeRuneInString(tag[pos:])
			if !pyIsSpace(r) && tag[pos] != '/' {
				break
			}
			pos += size
		}
		if pos >= n || tag[pos] == '>' {
			break
		}
		nameStart := pos
		for pos < n {
			r, size := utf8.DecodeRuneInString(tag[pos:])
			if pyIsSpace(r) || tag[pos] == '=' || tag[pos] == '/' || tag[pos] == '>' {
				break
			}
			pos += size
		}
		name := tag[nameStart:pos]
		pos = skipPySpaces(tag, pos)
		if pos >= n || tag[pos] != '=' {
			continue
		}
		pos = skipPySpaces(tag, pos+1)
		if pos < n && (tag[pos] == '"' || tag[pos] == '\'') {
			q := tag[pos]
			pos++
			valStart := pos
			for pos < n && tag[pos] != q {
				pos++
			}
			attrs = append(attrs, htmlAttr{name: name, start: valStart, end: pos})
			if pos < n {
				pos++
			}
		} else {
			valStart := pos
			for pos < n {
				r, size := utf8.DecodeRuneInString(tag[pos:])
				if pyIsSpace(r) || tag[pos] == '>' {
					break
				}
				pos += size
			}
			attrs = append(attrs, htmlAttr{name: name, start: valStart, end: pos})
		}
	}
	return attrs
}

func extractWithMask(text, maskedText string) []Reference {
	angleMatches := findAllAngleDestinations(maskedText)
	allTags := findAllHTMLTags(maskedText)

	if len(allTags) > 0 && len(angleMatches) > 0 {
		filtered := angleMatches[:0]
		tagIdx := 0
		for _, m := range angleMatches {
			for tagIdx < len(allTags) && allTags[tagIdx].end <= m.fullStart {
				tagIdx++
			}
			inside := tagIdx < len(allTags) &&
				allTags[tagIdx].start <= m.fullStart &&
				m.fullEnd <= allTags[tagIdx].end
			if !inside {
				filtered = append(filtered, m)
			}
		}
		angleMatches = filtered
	}

	angleSpans := make(map[byteSpan]bool, len(angleMatches))
	for _, m := range angleMatches {
		angleSpans[byteSpan{start: m.spanStart, end: m.spanEnd}] = true
	}

	var tags []byteSpan
	for _, tag := range allTags {
		if !angleSpans[tag] {
			tags = append(tags, tag)
		}
	}

	mdMasked := maskedText
	if len(tags) > 0 || len(angleMatches) > 0 {
		buf := []byte(maskedText)
		for _, tag := range tags {
			blankSpan(buf, tag.start, tag.end)
		}
		for _, m := range angleMatches {
			blankSpan(buf, m.fullStart, m.fullEnd)
		}
		mdMasked = string(buf)
	}

	out := scanMarkdownDestinations(text, mdMasked)
	for _, m := range angleMatches {
		out = append(out, Reference{
			Path:  trimPySpace(m.inner),
			Start: m.spanStart,
			End:   m.spanEnd,
			Kind:  "angle",
		})
	}
	for _, tag := range tags {
		tagStr := maskedText[tag.start:tag.end]
		for _, attr := range scanHTMLAttributes(tagStr) {
			low := strings.ToLower(attr.name)
			if low != "src" && low != "href" {
				continue
			}
			raw := tagStr[attr.start:attr.end]
			val := trimPySpace(raw)
			if val == "" {
				continue
			}
			lead := len(raw) - len(trimLeftPySpace(raw))
			start := tag.start + attr.start + lead
			end := start + len(val)
			out = append(out, Reference{
				Path:  html.UnescapeString(val),
				Start: start,
				End:   end,
				Kind:  "html",
			})
		}
	}

	filtered := out[:0]
	for _, r := range out {
		if maskedText[r.Start:r.End] == text[r.Start:r.End] {
			filtered = append(filtered, r)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].Start < filtered[j].Start
	})
	return filtered
}

func Extract(text string) []Reference {
	return extractWithMask(text, masked(text))
}

func isAutolink(tag string) bool {
	if len(tag) < 4 || tag[0] != '<' || tag[len(tag)-1] != '>' {
		return false
	}
	inner := tag[1 : len(tag)-1]
	if !isASCIIAlpha(inner[0]) {
		return false
	}
	colon := -1
	for i := 1; i < len(inner); i++ {
		ch := inner[i]
		if ch == ':' {
			colon = i
			break
		}
		if !isASCIIAlpha(ch) && (ch < '0' || ch > '9') && ch != '+' && ch != '.' && ch != '-' {
			return false
		}
	}
	if colon < 0 {
		return false
	}
	for _, r := range inner[colon+1:] {
		if pyIsSpace(r) || r == '<' || r == '>' {
			return false
		}
	}
	return true
}

func isEscaped(s string, pos int) bool {
	bs := 0
	for i := pos - 1; i >= 0 && s[i] == '\\'; i-- {
		bs++
	}
	return bs%2 == 1
}

func matchRefDefLine(s string, lineStart int) (string, int, bool) {
	pos := lineStart
	sp := 0
	for pos < len(s) && sp < 3 && s[pos] == ' ' {
		pos++
		sp++
	}
	if pos >= len(s) || s[pos] != '[' {
		return "", 0, false
	}
	pos++
	labelStart := pos
	for pos < len(s) && s[pos] != ']' && s[pos] != '\n' {
		pos++
	}
	if pos == labelStart || pos >= len(s) || s[pos] != ']' || pos+1 >= len(s) || s[pos+1] != ':' {
		return "", 0, false
	}
	pos += 2
	for pos < len(s) && (s[pos] == ' ' || s[pos] == '\t') {
		pos++
	}
	if pos >= len(s) || s[pos] == '\n' {
		return "", 0, false
	}
	var dest string
	if s[pos] == '<' {
		pos++
		dStart := pos
		for pos < len(s) && s[pos] != '>' && s[pos] != '\n' {
			pos++
		}
		if pos >= len(s) || s[pos] != '>' {
			return "", 0, false
		}
		dest = s[dStart:pos]
		pos++
	} else {
		dStart := pos
		for pos < len(s) {
			r, size := utf8.DecodeRuneInString(s[pos:])
			if pyIsSpace(r) {
				break
			}
			pos += size
		}
		dest = s[dStart:pos]
	}
	wsStart := pos
	for pos < len(s) && (s[pos] == ' ' || s[pos] == '\t') {
		pos++
	}
	if pos > wsStart && pos < len(s) && (s[pos] == '"' || s[pos] == '\'' || s[pos] == '(') {
		if next, ok := matchOptionalTitle(s, pos); ok {
			pos = next
		}
	}
	for pos < len(s) && (s[pos] == ' ' || s[pos] == '\t') {
		pos++
	}
	if pos < len(s) && s[pos] != '\n' && s[pos] != '\r' {
		return "", 0, false
	}
	return dest, pos, true
}

func scanReferenceDefinitions(maskedText string) []string {
	var out []string
	pos := 0
	for pos < len(maskedText) {
		lineEnd := strings.IndexByte(maskedText[pos:], '\n')
		var nextLine int
		if lineEnd < 0 {
			nextLine = len(maskedText)
		} else {
			nextLine = pos + lineEnd + 1
		}
		p := pos
		sp := 0
		for p < nextLine && sp < 3 && maskedText[p] == ' ' {
			p++
			sp++
		}
		if p < nextLine && maskedText[p] == '[' {
			p++
			labelStart := p
			for p < nextLine && maskedText[p] != ']' && maskedText[p] != '\n' {
				p++
			}
			if p > labelStart && p+1 < nextLine && maskedText[p] == ']' && maskedText[p+1] == ':' {
				p += 2
				for p < nextLine && (maskedText[p] == ' ' || maskedText[p] == '\t') {
					p++
				}
				var dest string
				if p < nextLine && maskedText[p] == '<' {
					p++
					dStart := p
					for p < nextLine && maskedText[p] != '>' && maskedText[p] != '\n' {
						p++
					}
					if p < nextLine && maskedText[p] == '>' {
						dest = trimPySpace(maskedText[dStart:p])
					}
				} else if p < nextLine && maskedText[p] != '\n' && maskedText[p] != '\r' {
					dStart := p
					for p < nextLine {
						r, size := utf8.DecodeRuneInString(maskedText[p:])
						if pyIsSpace(r) {
							break
						}
						p += size
					}
					dest = trimPySpace(maskedText[dStart:p])
				}
				if dest != "" {
					out = append(out, dest)
				}
			}
		}
		pos = nextLine
	}
	return out
}

func maskForRawHTML(text string) string {
	buf := []byte(text)
	pos := 0
	for pos < len(text) {
		f, hasFence := findNextFenceOpener(text, pos)
		codeLimit := len(text)
		if hasFence {
			codeLimit = f.start
		}
		c, hasCode := findNextCodeSpan(text, pos, codeLimit)
		cm, hasComment := findNextComment(text, pos)
		tg, hasTag := findNextHTMLTag(text, pos)

		bestStart := len(text) + 1
		bestKind := ""
		bestEnd := 0
		if hasFence && f.start < bestStart {
			bestStart = f.start
			bestKind = "fence"
			bestEnd = findFenceEnd(text, f)
		}
		if hasCode && c.start < bestStart {
			bestStart = c.start
			bestKind = "code"
			bestEnd = c.end
		}
		if hasComment && cm.start < bestStart {
			bestStart = cm.start
			bestKind = "comment"
			bestEnd = cm.end
		}
		if hasTag && tg.start < bestStart {
			bestStart = tg.start
			bestKind = "tag"
			bestEnd = tg.end
		}
		if bestKind == "" {
			break
		}
		if bestKind != "tag" {
			blankLinesSpan(buf, bestStart, bestEnd)
		}
		pos = bestEnd
	}

	s := string(buf)
	linePos := 0
	for linePos < len(s) {
		if _, end, ok := matchRefDefLine(s, linePos); ok {
			blankLinesSpan(buf, linePos, end)
		}
		nl := strings.IndexByte(s[linePos:], '\n')
		if nl < 0 {
			break
		}
		linePos += nl + 1
	}

	s = string(buf)
	for _, m := range findAllAngleDestinations(s) {
		blankSpan(buf, m.spanStart, m.fullEnd)
	}

	s = string(buf)
	consumedUntil := 0
	for i := 0; i < len(s); i++ {
		if i < consumedUntil {
			continue
		}
		if s[i] != '!' && s[i] != '[' {
			continue
		}
		start, ok := matchMarkdownOpenerAt(s, i)
		if !ok {
			continue
		}
		if s[i] == '!' && i+1 < len(s) && s[i+1] == '[' {
			i++
		}
		if start == len(s) || s[start] == '<' {
			continue
		}
		depth := 0
		cursor := start
		for cursor < len(s) {
			ch := s[cursor]
			if ch == '\\' && cursor+1 < len(s) {
				cursor += 2
				continue
			}
			if ch == '(' {
				depth++
				cursor++
				continue
			}
			if ch == ')' {
				if depth == 0 {
					blankSpan(buf, start, cursor)
					consumedUntil = cursor + 1
					break
				}
				depth--
				cursor++
				continue
			}
			r, size := utf8.DecodeRuneInString(s[cursor:])
			if ch == '<' || ch == '>' || ch == '\n' || (pyIsSpace(r) && depth == 0) {
				if titleEnd, ok := matchMarkdownTitle(s, cursor); ok {
					blankSpan(buf, start, titleEnd)
					consumedUntil = titleEnd
				}
				break
			}
			cursor += size
		}
	}
	return string(buf)
}

func HasRawHTML(text string) bool {
	m := maskForRawHTML(text)
	for _, tag := range findAllHTMLTags(m) {
		if !isAutolink(m[tag.start:tag.end]) && !isEscaped(m, tag.start) {
			return true
		}
	}
	return false
}
