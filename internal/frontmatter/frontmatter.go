package frontmatter

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxNestingDepth = 64

var PermissionLevels = map[string]int{
	"owner":       10,
	"owner+admin": 20,
	"team":        30,
	"account":     40,
	"public":      50,
}

var PermissionOrder = []string{"owner", "owner+admin", "team", "account", "public"}

var AllowedKeys = []string{
	"id",
	"entity",
	"title",
	"tags",
	"category",
	"status",
	"profile",
	"read",
	"write",
	"attachments",
}

var allowedKeySet = map[string]bool{
	"id":          true,
	"entity":      true,
	"title":       true,
	"tags":        true,
	"category":    true,
	"status":      true,
	"profile":     true,
	"read":        true,
	"write":       true,
	"attachments": true,
}

var utf8BOM = []byte{0xef, 0xbb, 0xbf}

type Meta struct {
	Values map[string]any
	Order  []string
}

func NewMeta() Meta {
	return Meta{Values: map[string]any{}}
}

func (m *Meta) Set(key string, val any) {
	if m.Values == nil {
		m.Values = map[string]any{}
	}
	if _, exists := m.Values[key]; !exists {
		m.Order = append(m.Order, key)
	}
	m.Values[key] = val
}

func (m Meta) Has(key string) bool {
	_, ok := m.Values[key]
	return ok
}

func (m Meta) Len() int {
	return len(m.Values)
}

func HasOpener(head []byte) bool {
	trimmed := bytes.TrimPrefix(head, utf8BOM)
	return bytes.HasPrefix(trimmed, []byte("---"))
}

func ValidateID(value any) (int, error) {
	switch v := value.(type) {
	case int:
		if v > 0 {
			return v, nil
		}
	case int64:
		if v > 0 {
			return int(v), nil
		}
	}
	return 0, errors.New("id must be a positive integer")
}

func ParseServerID(value any) (int, error) {
	switch v := value.(type) {
	case int:
		id, err := ValidateID(v)
		if err != nil {
			return 0, errors.New("server did not return a valid entity id")
		}
		return id, nil
	case int64:
		id, err := ValidateID(v)
		if err != nil {
			return 0, errors.New("server did not return a valid entity id")
		}
		return id, nil
	case string:
		if v == "" {
			return 0, errors.New("server did not return a valid entity id")
		}
		for i := range len(v) {
			if v[i] < '0' || v[i] > '9' {
				return 0, errors.New("server did not return a valid entity id")
			}
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, errors.New("server did not return a valid entity id")
		}
		id, err := ValidateID(n)
		if err != nil {
			return 0, errors.New("server did not return a valid entity id")
		}
		return id, nil
	default:
		return 0, errors.New("server did not return a valid entity id")
	}
}

func matchDelimiterAt(s string, pos int) (int, bool) {
	if !strings.HasPrefix(s[pos:], "---") {
		return 0, false
	}
	i := pos + 3
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	if i == len(s) {
		return i, true
	}
	if s[i] == '\n' {
		return i + 1, true
	}
	if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
		return i + 2, true
	}
	return 0, false
}

func findCloser(s string, from int) (int, int, bool) {
	pos := from
	for pos <= len(s) {
		if end, ok := matchDelimiterAt(s, pos); ok {
			return pos, end, true
		}
		nl := strings.IndexByte(s[pos:], '\n')
		if nl < 0 {
			break
		}
		pos += nl + 1
	}
	return 0, 0, false
}

func pyYAML11Bool(val string) (bool, bool) {
	switch val {
	case "y", "Y", "yes", "Yes", "YES", "true", "True", "TRUE", "on", "On", "ON":
		return true, true
	case "n", "N", "no", "No", "NO", "false", "False", "FALSE", "off", "Off", "OFF":
		return false, true
	default:
		return false, false
	}
}

func pyYAML11Null(val string) bool {
	switch val {
	case "", "~", "null", "Null", "NULL":
		return true
	default:
		return false
	}
}

func pyYAML11Int(val string) (int, bool) {
	if val == "" {
		return 0, false
	}
	sign := 1
	s := val
	if s[0] == '+' {
		s = s[1:]
	} else if s[0] == '-' {
		sign = -1
		s = s[1:]
	}
	if s == "" {
		return 0, false
	}
	clean := strings.ReplaceAll(s, "_", "")
	if clean == "" {
		return 0, false
	}
	if len(clean) > 1 && clean[0] == '0' && clean[1] >= '0' && clean[1] <= '7' {
		allOct := true
		for i := range len(clean) {
			if clean[i] < '0' || clean[i] > '7' {
				allOct = false
				break
			}
		}
		if allOct {
			n, err := strconv.ParseInt(clean, 8, 64)
			if err == nil {
				return sign * int(n), true
			}
		}
	}
	if strings.HasPrefix(clean, "0x") || strings.HasPrefix(clean, "0X") {
		n, err := strconv.ParseInt(clean[2:], 16, 64)
		if err == nil {
			return sign * int(n), true
		}
	}
	if strings.HasPrefix(clean, "0b") || strings.HasPrefix(clean, "0B") {
		n, err := strconv.ParseInt(clean[2:], 2, 64)
		if err == nil {
			return sign * int(n), true
		}
	}
	allDec := true
	for i := range len(clean) {
		if clean[i] < '0' || clean[i] > '9' {
			allDec = false
			break
		}
	}
	if allDec {
		n, err := strconv.ParseInt(clean, 10, 64)
		if err == nil {
			return sign * int(n), true
		}
	}
	return 0, false
}

func decodeScalar(n *yaml.Node) any {
	if n.Style == 0 {
		if n.Tag == "!!null" || pyYAML11Null(n.Value) {
			return nil
		}
		if b, ok := pyYAML11Bool(n.Value); ok {
			return b
		}
		if i, ok := pyYAML11Int(n.Value); ok {
			return i
		}
		if n.Tag == "!!float" {
			f, err := strconv.ParseFloat(strings.ReplaceAll(n.Value, "_", ""), 64)
			if err == nil {
				return f
			}
		}
		return n.Value
	}
	switch n.Tag {
	case "!!null":
		return nil
	case "!!bool":
		b, _ := strconv.ParseBool(n.Value)
		return b
	case "!!int":
		if i, ok := pyYAML11Int(n.Value); ok {
			return i
		}
	case "!!float":
		if f, err := strconv.ParseFloat(strings.ReplaceAll(n.Value, "_", ""), 64); err == nil {
			return f
		}
	}
	return n.Value
}

func decodeNode(n *yaml.Node, depth int) (any, error) {
	if depth > maxNestingDepth {
		return nil, errors.New("maximum nesting depth exceeded")
	}
	if n == nil {
		return nil, nil
	}
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return decodeNode(n.Content[0], depth+1)
	case yaml.AliasNode:
		return nil, errors.New("found an alias")
	case yaml.ScalarNode:
		return decodeScalar(n), nil
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, child := range n.Content {
			v, err := decodeNode(child, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.MappingNode:
		m := NewMeta()
		for i := 0; i+1 < len(n.Content); i += 2 {
			kNode := n.Content[i]
			vNode := n.Content[i+1]
			if kNode.Kind == yaml.AliasNode {
				return nil, errors.New("found an alias")
			}
			if kNode.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("found unhashable key at line %d", kNode.Line)
			}
			kv := decodeScalar(kNode)
			ks := pyString(kv)
			if m.Has(ks) {
				return nil, fmt.Errorf("found duplicate key %q", ks)
			}
			val, err := decodeNode(vNode, depth+1)
			if err != nil {
				return nil, err
			}
			m.Set(ks, val)
		}
		return m, nil
	default:
		return nil, nil
	}
}

func pyString(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		if math.Trunc(x) == x && !math.IsInf(x, 0) && !math.IsNaN(x) {
			return fmt.Sprintf("%.1f", x)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

func checkPreParseDepth(raw string) error {
	depth := 0
	inSingle := false
	inDouble := false
	escaped := false
	for i := range len(raw) {
		ch := raw[i]
		if escaped {
			escaped = false
			continue
		}
		if inSingle {
			if ch == '\'' {
				inSingle = false
			}
			continue
		}
		if inDouble {
			if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inDouble = false
			}
			continue
		}
		switch ch {
		case '\'':
			inSingle = true
		case '"':
			inDouble = true
		case '[', '{':
			depth++
			if depth > maxNestingDepth {
				return errors.New("maximum nesting depth exceeded")
			}
		case ']', '}':
			if depth > 0 {
				depth--
			}
		}
	}
	return nil
}

func Parse(text string) (Meta, string, error) {
	document := strings.TrimPrefix(text, "\ufeff")
	openerEnd, ok := matchDelimiterAt(document, 0)
	if !ok {
		return NewMeta(), text, nil
	}
	closerStart, closerEnd, ok := findCloser(document, openerEnd)
	if !ok {
		return NewMeta(), text, nil
	}
	rawYAML := document[openerEnd:closerStart]
	if err := checkPreParseDepth(rawYAML); err != nil {
		return NewMeta(), "", fmt.Errorf("invalid frontmatter YAML: %w", err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(rawYAML), &root); err != nil {
		return NewMeta(), "", fmt.Errorf("invalid frontmatter YAML: %w", err)
	}
	decoded, err := decodeNode(&root, 0)
	if err != nil {
		return NewMeta(), "", fmt.Errorf("invalid frontmatter YAML: %w", err)
	}
	meta, ok := decoded.(Meta)
	if !ok {
		return NewMeta(), text, nil
	}

	var unknown []string
	for k := range meta.Values {
		if !allowedKeySet[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return NewMeta(), "", fmt.Errorf("unknown front matter key(s): %s", strings.Join(unknown, ", "))
	}

	if v, exists := meta.Values["id"]; exists {
		id, err := ValidateID(v)
		if err != nil {
			return NewMeta(), "", err
		}
		meta.Values["id"] = id
	}
	for _, key := range []string{"title", "profile"} {
		if v, exists := meta.Values[key]; exists && v != nil {
			meta.Values[key] = pyString(v)
		}
	}
	for _, key := range []string{"read", "write"} {
		if v, exists := meta.Values[key]; exists {
			s, ok := v.(string)
			if !ok || PermissionLevels[s] == 0 {
				return NewMeta(), "", fmt.Errorf("%s must be one of: %s", key, strings.Join(PermissionOrder, ", "))
			}
		}
	}
	if v, exists := meta.Values["tags"]; exists {
		switch tv := v.(type) {
		case nil:
			meta.Values["tags"] = []any{}
		case []any:
		case Meta:
			return NewMeta(), "", errors.New("tags must be a list or scalar")
		default:
			meta.Values["tags"] = []any{tv}
		}
	}
	if v, exists := meta.Values["attachments"]; exists {
		var entries []any
		switch av := v.(type) {
		case nil:
			entries = []any{}
		case []any:
			entries = av
		default:
			entries = []any{av}
		}
		out := make([]string, 0, len(entries))
		for _, entry := range entries {
			switch e := entry.(type) {
			case string:
				out = append(out, e)
			case int:
				out = append(out, strconv.Itoa(e))
			default:
				return NewMeta(), "", errors.New("attachments must be a list of paths")
			}
		}
		meta.Values["attachments"] = out
	}

	body := document[closerEnd:]
	if strings.HasPrefix(body, "\r\n") {
		body = body[2:]
	} else if strings.HasPrefix(body, "\r") || strings.HasPrefix(body, "\n") {
		body = body[1:]
	}
	return meta, body, nil
}

func TextMeta(text string) (Meta, bool) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	meta, _, err := Parse(normalized)
	if err != nil {
		return NewMeta(), false
	}
	if !meta.Has("id") {
		return NewMeta(), false
	}
	profile, ok := meta.Values["profile"].(string)
	if !ok || profile == "" {
		return NewMeta(), false
	}
	return meta, true
}

func DocumentMeta(path string) (Meta, bool) {
	f, err := os.Open(path)
	if err != nil {
		return NewMeta(), false
	}
	var head [6]byte
	n, err := f.Read(head[:])
	_ = f.Close()
	if err != nil && n == 0 {
		return NewMeta(), false
	}
	if !HasOpener(head[:n]) {
		return NewMeta(), false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return NewMeta(), false
	}
	return TextMeta(string(raw))
}

func needsSingleQuote(s string) bool {
	if s == "" {
		return true
	}
	if pyYAML11Null(s) {
		return true
	}
	if _, ok := pyYAML11Bool(s); ok {
		return true
	}
	if _, ok := pyYAML11Int(s); ok {
		return true
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return true
	}
	if strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") || strings.HasPrefix(s, "\t") || strings.HasSuffix(s, "\t") {
		return true
	}
	first := s[0]
	if strings.ContainsRune("-?:,[]{}#&*!|>'\"%@`", rune(first)) {
		return true
	}
	for i := range len(s) {
		ch := s[i]
		if ch == '\n' || ch == '\r' || ch == '\t' || ch < 0x20 {
			return true
		}
		if ch == ':' && (i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\t') {
			return true
		}
		if ch == '#' && i > 0 && s[i-1] == ' ' {
			return true
		}
	}
	return false
}

func emitSingleQuoted(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for i := range len(s) {
		switch s[i] {
		case '\'':
			b.WriteString("''")
		case '\n':
			b.WriteString("\n\n  ")
		default:
			b.WriteByte(s[i])
		}
	}
	b.WriteByte('\'')
	return b.String()
}

func emitScalar(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case string:
		if needsSingleQuote(x) {
			return emitSingleQuoted(x)
		}
		return x
	default:
		s := fmt.Sprint(x)
		if needsSingleQuote(s) {
			return emitSingleQuoted(s)
		}
		return s
	}
}

func Render(meta Meta, body string) string {
	if len(meta.Values) == 0 {
		return "---\n{}\n---\n\n" + body
	}
	keys := meta.Order
	if len(keys) == 0 {
		for k := range meta.Values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	}
	var b strings.Builder
	b.WriteString("---\n")
	for _, k := range keys {
		v, ok := meta.Values[k]
		if !ok {
			continue
		}
		switch list := v.(type) {
		case []string:
			if len(list) == 0 {
				fmt.Fprintf(&b, "%s: []\n", k)
			} else {
				fmt.Fprintf(&b, "%s:\n", k)
				for _, item := range list {
					fmt.Fprintf(&b, "- %s\n", emitScalar(item))
				}
			}
		case []any:
			if len(list) == 0 {
				fmt.Fprintf(&b, "%s: []\n", k)
			} else {
				fmt.Fprintf(&b, "%s:\n", k)
				for _, item := range list {
					fmt.Fprintf(&b, "- %s\n", emitScalar(item))
				}
			}
		default:
			fmt.Fprintf(&b, "%s: %s\n", k, emitScalar(v))
		}
	}
	b.WriteString("---\n\n")
	b.WriteString(body)
	return b.String()
}

func KeysEqual(a, b []string) bool {
	return slices.Equal(a, b)
}
