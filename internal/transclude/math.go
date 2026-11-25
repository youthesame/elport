package transclude

import "strings"

type MathSpan struct {
	Start   int
	End     int
	Display bool
}

func isBackslashEscaped(s string, pos int) bool {
	count := 0
	for i := pos - 1; i >= 0 && s[i] == '\\'; i-- {
		count++
	}
	return count%2 == 1
}

func hasBlankLineBetween(s string, start, end int) bool {
	lineBlank := false
	for i := start; i < end; i++ {
		if s[i] == '\n' {
			if lineBlank {
				return true
			}
			lineBlank = true
			continue
		}
		if s[i] != ' ' && s[i] != '\t' && s[i] != '\r' {
			lineBlank = false
		}
	}
	return false
}

func advanceTeXEscape(s string, pos int) int {
	if pos+1 >= len(s) {
		return pos + 1
	}
	if isASCIIAlpha(s[pos+1]) {
		j := pos + 1
		for j < len(s) && isASCIIAlpha(s[j]) {
			j++
		}
		return j
	}
	return pos + 2
}

func findMathEnd(s string, start int, closeSeq string, allowBlankLine bool) int {
	braces := 0
	i := start
	for i < len(s) {
		if !allowBlankLine && s[i] == '\n' && hasBlankLineBetween(s, start, i+1) {
			return -1
		}
		if braces == 0 && i+len(closeSeq) <= len(s) && s[i:i+len(closeSeq)] == closeSeq {
			if closeSeq == "$" && i == start {
				return -1
			}
			return i
		}
		ch := s[i]
		if ch == '\\' {
			i = advanceTeXEscape(s, i)
			continue
		}
		if ch == '{' {
			braces++
		} else if ch == '}' && braces > 0 {
			braces--
		}
		i++
	}
	return -1
}

func maskReferenceDestinations(text, baseMask string) string {
	refs := extractWithMask(text, baseMask)
	if len(refs) == 0 {
		return baseMask
	}
	buf := []byte(baseMask)
	for _, r := range refs {
		blankSpan(buf, r.Start, r.End)
	}
	return string(buf)
}

func ScanMath(text string) ([]MathSpan, []int) {
	m := maskReferenceDestinations(text, masked(text))
	var spans []MathSpan
	var escapedDollars []int
	pos := 0
	for pos < len(m) {
		if m[pos] == '\\' && pos+1 < len(m) && !isBackslashEscaped(m, pos) {
			next := m[pos+1]
			if next == '$' {
				escapedDollars = append(escapedDollars, pos)
				pos += 2
				continue
			}
			if next == '[' {
				if closeIdx := findMathEnd(m, pos+2, `\]`, true); closeIdx >= 0 {
					spans = append(spans, MathSpan{Start: pos, End: closeIdx + 2, Display: true})
					pos = closeIdx + 2
					continue
				}
				pos += 2
				continue
			}
			if next == '(' {
				if closeIdx := findMathEnd(m, pos+2, `\)`, false); closeIdx >= 0 {
					spans = append(spans, MathSpan{Start: pos, End: closeIdx + 2, Display: false})
					pos = closeIdx + 2
					continue
				}
				pos += 2
				continue
			}
		}
		if m[pos] == '$' && !isBackslashEscaped(m, pos) {
			if pos+1 < len(m) && m[pos+1] == '$' {
				if closeIdx := findMathEnd(m, pos+2, "$$", true); closeIdx >= 0 {
					spans = append(spans, MathSpan{Start: pos, End: closeIdx + 2, Display: true})
					pos = closeIdx + 2
					continue
				}
				pos += 2
				continue
			}
			if closeIdx := findMathEnd(m, pos+1, "$", false); closeIdx >= 0 {
				spans = append(spans, MathSpan{Start: pos, End: closeIdx + 1, Display: false})
				pos = closeIdx + 1
				continue
			}
		}
		pos++
	}
	return spans, escapedDollars
}

func maskedWithMath(text string) string {
	m := masked(text)
	spans, _ := ScanMath(text)
	if len(spans) == 0 {
		return m
	}
	buf := []byte(m)
	for _, sp := range spans {
		blankSpan(buf, sp.Start, sp.End)
	}
	return string(buf)
}

func trimSpaceBytes(s string) string {
	return strings.TrimSpace(s)
}
