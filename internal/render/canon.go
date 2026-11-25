package render

import (
	"html"
	"sort"
	"strings"

	xnethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var blockTags = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true,
	"details": true, "div": true, "dl": true, "dt": true, "dd": true,
	"figcaption": true, "figure": true, "footer": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "header": true, "hr": true,
	"li": true, "main": true, "nav": true, "ol": true, "p": true, "pre": true,
	"section": true, "summary": true, "table": true, "tbody": true, "tfoot": true,
	"thead": true, "tr": true, "ul": true,
}

var voidTags = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

var preformattedTags = map[string]bool{
	"pre": true, "code": true, "textarea": true, "script": true, "style": true,
}

var booleanAttrs = map[string]bool{
	"checked": true, "disabled": true, "hidden": true, "ismap": true,
	"multiple": true, "novalidate": true, "open": true, "readonly": true,
	"required": true, "reversed": true, "selected": true,
}

func hasWhiteSpacePre(attrs []xnethtml.Attribute) bool {
	for _, a := range attrs {
		if strings.ToLower(a.Key) != "style" {
			continue
		}
		for _, decl := range strings.Split(a.Val, ";") {
			k, v, ok := strings.Cut(decl, ":")
			if !ok {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(k), "white-space") {
				val := strings.ToLower(strings.TrimSpace(v))
				if strings.HasPrefix(val, "pre") {
					return true
				}
			}
		}
	}
	return false
}

func normalizeStyleAttr(val string) string {
	var parts []string
	for _, decl := range strings.Split(val, ";") {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		k, v, ok := strings.Cut(decl, ":")
		if !ok {
			parts = append(parts, collapseSpaces(decl))
			continue
		}
		parts = append(parts, strings.ToLower(strings.TrimSpace(k))+":"+collapseSpaces(strings.TrimSpace(v)))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ";") + ";"
}

func collapseSpaces(s string) string {
	var b strings.Builder
	inSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' {
			if !inSpace {
				b.WriteByte(' ')
				inSpace = true
			}
			continue
		}
		b.WriteRune(r)
		inSpace = false
	}
	return b.String()
}

func writeAttrs(b *strings.Builder, tag string, attrs []xnethtml.Attribute) {
	if len(attrs) == 0 {
		return
	}
	type kv struct {
		key  string
		val  string
		flag bool
	}
	kept := make([]kv, 0, len(attrs))
	for _, a := range attrs {
		k := strings.ToLower(a.Key)
		if tag == "img" && k == "alt" {
			continue
		}
		v := a.Val
		if k == "style" {
			v = normalizeStyleAttr(v)
			if v == "" {
				continue
			}
		}
		if booleanAttrs[k] && (v == "" || strings.EqualFold(v, k)) {
			kept = append(kept, kv{key: k, flag: true})
			continue
		}
		kept = append(kept, kv{key: k, val: v})
	}
	sort.Slice(kept, func(i, j int) bool {
		if kept[i].key != kept[j].key {
			return kept[i].key < kept[j].key
		}
		return kept[i].val < kept[j].val
	})
	for _, item := range kept {
		b.WriteByte(' ')
		b.WriteString(item.key)
		if !item.flag {
			b.WriteString(`="`)
			b.WriteString(html.EscapeString(item.val))
			b.WriteByte('"')
		}
	}
}

func renderCanonNode(b *strings.Builder, n *xnethtml.Node, preDepth int, parentIsBlock bool) {
	switch n.Type {
	case xnethtml.CommentNode:
		return
	case xnethtml.TextNode:
		if preDepth > 0 {
			b.WriteString(html.EscapeString(n.Data))
			return
		}
		collapsed := collapseSpaces(n.Data)
		prevBlock := (n.PrevSibling == nil && parentIsBlock) || (n.PrevSibling != nil && n.PrevSibling.Type == xnethtml.ElementNode && blockTags[strings.ToLower(n.PrevSibling.Data)])
		nextBlock := (n.NextSibling == nil && parentIsBlock) || (n.NextSibling != nil && n.NextSibling.Type == xnethtml.ElementNode && blockTags[strings.ToLower(n.NextSibling.Data)])
		if collapsed == " " && (prevBlock || nextBlock) {
			return
		}
		if prevBlock {
			collapsed = strings.TrimLeft(collapsed, " ")
		}
		if nextBlock {
			collapsed = strings.TrimRight(collapsed, " ")
		}
		if collapsed != "" {
			b.WriteString(html.EscapeString(collapsed))
		}
	case xnethtml.ElementNode:
		tag := strings.ToLower(n.Data)
		if tag == "thead" || tag == "tbody" {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				renderCanonNode(b, c, preDepth, true)
			}
			return
		}
		isBlock := blockTags[tag]
		if isBlock && preDepth == 0 && b.Len() > 0 && b.String()[b.Len()-1] != '\n' {
			b.WriteByte('\n')
		}
		b.WriteByte('<')
		b.WriteString(tag)
		writeAttrs(b, tag, n.Attr)
		b.WriteByte('>')
		if voidTags[tag] {
			if isBlock && preDepth == 0 {
				b.WriteByte('\n')
			}
			return
		}
		nextPre := preDepth
		if preformattedTags[tag] || hasWhiteSpacePre(n.Attr) {
			nextPre++
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			renderCanonNode(b, c, nextPre, isBlock)
		}
		b.WriteString("</")
		b.WriteString(tag)
		b.WriteByte('>')
		if isBlock && preDepth == 0 {
			b.WriteByte('\n')
		}
	}
}

func CanonHTML(src string) string {
	ctx := &xnethtml.Node{
		Type:     xnethtml.ElementNode,
		DataAtom: atom.Body,
		Data:     "body",
	}
	nodes, err := xnethtml.ParseFragment(strings.NewReader(src), ctx)
	if err != nil {
		return src
	}
	var b strings.Builder
	for _, n := range nodes {
		renderCanonNode(&b, n, 0, true)
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
