package sync

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/youthesame/elport/internal/frontmatter"
	"github.com/youthesame/elport/internal/transclude"
)

var permissionAudiences = map[string]string{
	"account": "everyone with an account",
	"public":  "everyone incl. anonymous; no login required",
}

func isAllASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func resolveCatalogID(entries []map[string]any, value any, kind string) (int, error) {
	if value == nil {
		return 0, nil
	}
	var target int
	isNumeric := false
	switch v := value.(type) {
	case int:
		target = v
		isNumeric = true
	case int64:
		target = int(v)
		isNumeric = true
	case json.Number:
		if n, err := v.Int64(); err == nil {
			target = int(n)
			isNumeric = true
		}
	case string:
		if isAllASCIIDigits(v) {
			if n, err := strconv.Atoi(v); err == nil {
				target = n
				isNumeric = true
			}
		}
	}
	if isNumeric {
		for _, entry := range entries {
			if id, ok := intField(entry, "id"); ok && id == target {
				return target, nil
			}
		}
		return 0, fmt.Errorf("%s not found: %v", kind, value)
	}
	want := fmt.Sprint(value)
	for _, entry := range entries {
		if strField(entry, "title") == want {
			if id, ok := intField(entry, "id"); ok {
				return id, nil
			}
		}
	}
	return 0, fmt.Errorf("%s not found: %v", kind, value)
}

func remoteTags(remote map[string]any) map[string]bool {
	out := map[string]bool{}
	if remote == nil {
		return out
	}
	if decoded, ok := remote["tags_decoded"].([]any); ok && len(decoded) > 0 {
		for _, item := range decoded {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			tag := strings.TrimSpace(strField(m, "tag"))
			if tag != "" {
				out[tag] = true
			}
		}
		return out
	}
	raw, ok := remote["tags"]
	if !ok || raw == nil {
		return out
	}
	switch v := raw.(type) {
	case string:
		for _, part := range strings.FieldsFunc(v, func(r rune) bool { return r == '|' || r == ',' }) {
			if t := strings.TrimSpace(part); t != "" {
				out[t] = true
			}
		}
	case []any:
		for _, item := range v {
			if item == nil {
				continue
			}
			if t := strings.TrimSpace(fmt.Sprint(item)); t != "" {
				out[t] = true
			}
		}
	case []string:
		for _, item := range v {
			if t := strings.TrimSpace(item); t != "" {
				out[t] = true
			}
		}
	}
	return out
}

func syncedMeta(meta frontmatter.Meta) map[string]any {
	out := map[string]any{}
	for _, key := range SyncedMetaKeys {
		if v, exists := meta.Values[key]; exists {
			out[key] = v
		}
	}
	return out
}

func remoteMeta(remoteDoc map[string]any) map[string]any {
	levels := map[int]string{}
	for kw, lvl := range frontmatter.PermissionLevels {
		levels[lvl] = kw
	}
	catalog := func(field string) any {
		rawTitle := remoteDoc[field+"_title"]
		if rawTitle == nil {
			return nil
		}
		title := fmt.Sprint(rawTitle)
		if isAllASCIIDigits(title) {
			v := remoteDoc[field]
			if v == nil || fmt.Sprint(v) == "" || fmt.Sprint(v) == "0" {
				return nil
			}
			if n, ok := intField(remoteDoc, field); ok {
				return n
			}
			return v
		}
		if title == "" {
			return nil
		}
		return title
	}
	out := map[string]any{}
	if v, ok := remoteDoc["title"]; ok {
		s := strField(remoteDoc, "title")
		if v == nil || s == "" {
			out["title"] = nil
		} else {
			out["title"] = s
		}
	}
	if _, ok := remoteDoc["category_title"]; ok {
		out["category"] = catalog("category")
	}
	if _, ok := remoteDoc["status_title"]; ok {
		out["status"] = catalog("status")
	}
	if _, ok := remoteDoc["canread_base"]; ok {
		if lvl, ok := intField(remoteDoc, "canread_base"); ok && levels[lvl] != "" {
			out["read"] = levels[lvl]
		} else {
			out["read"] = nil
		}
	}
	if _, ok := remoteDoc["canwrite_base"]; ok {
		if lvl, ok := intField(remoteDoc, "canwrite_base"); ok && levels[lvl] != "" {
			out["write"] = levels[lvl]
		} else {
			out["write"] = nil
		}
	}
	return out
}

func metaTags(meta frontmatter.Meta) []string {
	raw, ok := meta.Values["tags"]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		out := make([]string, len(v))
		copy(out, v)
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, fmt.Sprint(item))
		}
		return out
	default:
		return []string{fmt.Sprint(v)}
	}
}

func mergeMeta(meta frontmatter.Meta, base map[string]any, remoteDoc map[string]any) (frontmatter.Meta, map[string]any) {
	merged := meta.Clone()
	newBase := make(map[string]any, len(base))
	for k, v := range base {
		newBase[k] = v
	}
	rm := remoteMeta(remoteDoc)
	for _, key := range SyncedMetaKeys {
		val, present := rm[key]
		if !present {
			continue
		}
		if val == nil {
			delete(newBase, key)
		} else {
			newBase[key] = val
		}
		if SameMeta(meta.Values[key], base[key]) && !SameMeta(meta.Values[key], val) {
			if val == nil {
				merged.Delete(key)
			} else {
				merged.Set(key, val)
			}
		}
	}
	if _, hasTags := remoteDoc["tags"]; hasTags {
		union := remoteTags(remoteDoc)
		for _, t := range metaTags(meta) {
			union[t] = true
		}
		if len(union) == 0 {
			merged.Delete("tags")
		} else {
			sorted := make([]string, 0, len(union))
			for t := range union {
				sorted = append(sorted, t)
			}
			sort.Strings(sorted)
			merged.Set("tags", sorted)
		}
	}
	return merged, newBase
}

func rememberRemotePaths(saved map[string]any, bodies ...string) []string {
	seen := savedStringList(saved, "remote_paths")
	inSeen := make(map[string]bool, len(seen))
	for _, v := range seen {
		inSeen[v] = true
	}
	for _, body := range bodies {
		for _, value := range transclude.BarePaths(body) {
			if !inSeen[value] {
				inSeen[value] = true
				seen = append(seen, value)
			}
		}
	}
	return seen
}

func carriedState(saved map[string]any) map[string]any {
	out := map[string]any{}
	if saved == nil {
		return out
	}
	if dl := savedDocLinks(saved); len(dl) > 0 {
		out["doc_links"] = dl
	}
	if links := savedLinks(saved); len(links) > 0 {
		out["links"] = links
	}
	return out
}

func savedLinks(saved map[string]any) [][]any {
	if saved == nil {
		return nil
	}
	raw, ok := saved["links"].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make([][]any, 0, len(raw))
	for _, item := range raw {
		pair, ok := item.([]any)
		if !ok || len(pair) != 2 {
			continue
		}
		ent, ok := pair[0].(string)
		if !ok {
			continue
		}
		id, ok := anyToInt(pair[1])
		if !ok {
			continue
		}
		out = append(out, []any{ent, id})
	}
	return out
}

func anyToInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case json.Number:
		n, err := x.Int64()
		return int(n), err == nil
	case string:
		n, err := strconv.Atoi(x)
		return n, err == nil
	default:
		return 0, false
	}
}

func savedMap(saved map[string]any, key string) map[string]any {
	if saved == nil {
		return map[string]any{}
	}
	m, ok := saved[key].(map[string]any)
	if !ok || m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func hasKey(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	_, ok := m[key]
	return ok
}

func metaEqual(a, b frontmatter.Meta) bool {
	if len(a.Values) != len(b.Values) {
		return false
	}
	for k, va := range a.Values {
		vb, ok := b.Values[k]
		if !ok {
			return false
		}
		if !metaValEqual(va, vb) {
			return false
		}
	}
	return true
}

func metaValEqual(a, b any) bool {
	sa, aIsSlice := stringSlice(a)
	sb, bIsSlice := stringSlice(b)
	if aIsSlice || bIsSlice {
		if !aIsSlice || !bIsSlice || len(sa) != len(sb) {
			return false
		}
		for i := range sa {
			if sa[i] != sb[i] {
				return false
			}
		}
		return true
	}
	return SameMeta(a, b)
}

func stringSlice(v any) ([]string, bool) {
	switch s := v.(type) {
	case []string:
		return s, true
	case []any:
		out := make([]string, len(s))
		for i, item := range s {
			out[i] = fmt.Sprint(item)
		}
		return out, true
	default:
		return nil, false
	}
}

func mapEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || !SameMeta(va, vb) {
			return false
		}
	}
	return true
}

func remoteValEqual(remoteVal, payloadVal any) bool {
	if remoteVal == nil || payloadVal == nil {
		return remoteVal == nil && payloadVal == nil
	}
	if pi, ok := anyToInt(payloadVal); ok {
		if ri, rok := anyToInt(remoteVal); rok {
			return ri == pi
		}
		return false
	}
	return fmt.Sprint(remoteVal) == fmt.Sprint(payloadVal)
}

func docLinksEqual(a, b map[string]map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, ma := range a {
		mb, ok := b[k]
		if !ok || len(ma) != len(mb) {
			return false
		}
		for fk, fv := range ma {
			if mb[fk] != fv {
				return false
			}
		}
	}
	return true
}
