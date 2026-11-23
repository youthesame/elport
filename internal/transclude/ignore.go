package transclude

import "strings"

type IgnoreLayer struct {
	Patterns []string
	Prefix   string
}

func fnmatchCase(name, pattern string) bool {
	nRunes := []rune(name)
	pRunes := []rune(pattern)
	return fnmatchRunes(nRunes, pRunes)
}

func fnmatchRunes(name, pattern []rune) bool {
	ni, pi := 0, 0
	starP, starN := -1, 0
	for ni < len(name) {
		if pi < len(pattern) && pattern[pi] == '*' {
			starP = pi
			pi++
			starN = ni
			continue
		}
		if pi < len(pattern) && pattern[pi] == '?' {
			pi++
			ni++
			continue
		}
		if pi < len(pattern) && pattern[pi] == '[' {
			matched, nextP, ok := matchCharClass(name[ni], pattern, pi)
			if ok {
				if matched {
					pi = nextP
					ni++
					continue
				}
			} else if name[ni] == '[' {
				pi++
				ni++
				continue
			}
		} else if pi < len(pattern) && pattern[pi] == name[ni] {
			pi++
			ni++
			continue
		}
		if starP >= 0 {
			pi = starP + 1
			starN++
			ni = starN
			continue
		}
		return false
	}
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}

func matchCharClass(ch rune, pattern []rune, start int) (bool, int, bool) {
	i := start + 1
	if i >= len(pattern) {
		return false, start, false
	}
	negated := false
	if pattern[i] == '!' {
		negated = true
		i++
	}
	j := i
	if j < len(pattern) && pattern[j] == ']' {
		j++
	}
	for j < len(pattern) && pattern[j] != ']' {
		j++
	}
	if j >= len(pattern) {
		return false, start, false
	}
	matched := false
	for k := i; k < j; {
		if k+2 < j && pattern[k+1] == '-' {
			lo, hi := pattern[k], pattern[k+2]
			if lo <= ch && ch <= hi {
				matched = true
			}
			k += 3
		} else {
			if pattern[k] == ch {
				matched = true
			}
			k++
		}
	}
	if negated {
		matched = !matched
	}
	return matched, j + 1, true
}

func pathMatches(relative, pattern string) bool {
	pParts := strings.Split(pattern, "/")
	rParts := strings.Split(relative, "/")
	rows := len(pParts) + 1
	cols := len(rParts) + 1
	memo := make([]int8, rows*cols)

	var match func(pi, ri int) bool
	match = func(pi, ri int) bool {
		idx := pi*cols + ri
		if memo[idx] != 0 {
			return memo[idx] == 1
		}
		var res bool
		if pi == len(pParts) {
			res = ri == len(rParts)
		} else if pParts[pi] == "**" {
			res = match(pi+1, ri) || (ri < len(rParts) && match(pi, ri+1))
		} else {
			res = ri < len(rParts) && fnmatchCase(rParts[ri], pParts[pi]) && match(pi+1, ri+1)
		}
		if res {
			memo[idx] = 1
		} else {
			memo[idx] = -1
		}
		return res
	}
	return match(0, 0)
}

func Matches(relative string, patterns []string) bool {
	parts := strings.Split(relative, "/")
	for _, raw := range patterns {
		pattern := strings.ReplaceAll(raw, "\\", "/")
		rooted := strings.HasPrefix(pattern, "/")
		pattern = strings.TrimPrefix(pattern, "/")
		dirOnly := strings.HasSuffix(pattern, "/")
		pattern = strings.TrimSuffix(pattern, "/")
		if pattern == "" {
			continue
		}
		if !rooted && !strings.Contains(pattern, "/") {
			segments := parts
			if dirOnly {
				segments = parts[:len(parts)-1]
			}
			for _, seg := range segments {
				if fnmatchCase(seg, pattern) {
					return true
				}
			}
			continue
		}
		limit := len(parts) + 1
		if dirOnly {
			limit = len(parts)
		}
		for count := 1; count < limit; count++ {
			if pathMatches(strings.Join(parts[:count], "/"), pattern) {
				return true
			}
		}
	}
	return false
}

func Ignored(relative string, layers []IgnoreLayer) bool {
	for _, layer := range layers {
		target := relative
		if layer.Prefix != "" {
			target = layer.Prefix + "/" + relative
		}
		if Matches(target, layer.Patterns) {
			return true
		}
	}
	return false
}
