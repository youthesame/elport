package render

import (
	"bytes"
	"fmt"
	"html"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"

	"github.com/youthesame/elport/internal/transclude"
)

type sTagStrikethroughRenderer struct{}

func (r *sTagStrikethroughRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(extast.KindStrikethrough, r.renderStrikethrough)
}

func (r *sTagStrikethroughRenderer) renderStrikethrough(w util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("<s>")
	} else {
		_, _ = w.WriteString("</s>")
	}
	return ast.WalkContinue, nil
}

var markdownEngine = goldmark.New(
	goldmark.WithExtensions(
		extension.NewTable(
			extension.WithTableCellAlignMethod(extension.TableCellAlignStyle),
		),
		extension.Strikethrough,
		extension.Linkify,
	),
	goldmark.WithRendererOptions(
		gmhtml.WithUnsafe(),
		renderer.WithNodeRenderers(
			util.Prioritized(&sTagStrikethroughRenderer{}, 100),
		),
	),
)

type replacementItem struct {
	start int
	end   int
	value string
}

func uniquePlaceholderPrefix(src string) string {
	prefix := "ELPORTTEX"
	for strings.Contains(src, prefix) {
		prefix += "X"
	}
	return prefix
}

func protectMath(md string) (string, map[string]string) {
	spans, escapedDollars := transclude.ScanMath(md)
	if len(spans) == 0 && len(escapedDollars) == 0 {
		return md, nil
	}
	items := make([]replacementItem, 0, len(spans)+len(escapedDollars))
	for _, sp := range spans {
		items = append(items, replacementItem{
			start: sp.Start,
			end:   sp.End,
			value: html.EscapeString(md[sp.Start:sp.End]),
		})
	}
	for _, pos := range escapedDollars {
		items = append(items, replacementItem{
			start: pos,
			end:   pos + 2,
			value: `\$`,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].start < items[j].start
	})

	prefix := uniquePlaceholderPrefix(md)
	tokens := make(map[string]string, len(items))
	var b strings.Builder
	pos := 0
	for i, it := range items {
		if it.start < pos {
			continue
		}
		tok := fmt.Sprintf("%s%dEND", prefix, i)
		tokens[tok] = it.value
		b.WriteString(md[pos:it.start])
		b.WriteString(tok)
		pos = it.end
	}
	b.WriteString(md[pos:])
	return b.String(), tokens
}

func MarkdownToHTML(md string) (string, error) {
	protected, tokens := protectMath(md)
	var buf bytes.Buffer
	if err := markdownEngine.Convert([]byte(protected), &buf); err != nil {
		return "", err
	}
	out := buf.String()
	if len(tokens) > 0 {
		pairs := make([]string, 0, len(tokens)*2)
		for tok, val := range tokens {
			pairs = append(pairs, tok, val)
		}
		out = strings.NewReplacer(pairs...).Replace(out)
	}
	return out, nil
}
