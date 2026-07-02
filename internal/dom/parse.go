package dom

import (
	"errors"
	"fmt"
	"io"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/xml"
)

// maxDepth bounds element nesting so hostile input cannot exhaust the stack
// during tree walks. Real SVG rarely nests beyond a few dozen levels.
const maxDepth = 10000

// Parse builds a document tree from svg. It fails on malformed markup
// (mismatched or unclosed tags, no root element) so the caller can fall back
// to the untouched input.
func Parse(svg []byte) (*Node, error) {
	// The lexer normalizes whitespace inside attribute values in place and
	// may write one byte past the slice; give it a private copy and keep the
	// caller's bytes for verbatim raw slices.
	work := append(make([]byte, 0, len(svg)+1), svg...)
	in := parse.NewInputBytes(work)
	l := xml.NewLexer(in)

	doc := &Node{Kind: KindDocument}
	cur := doc
	depth := 0
	var open *Node // node whose start tag is being lexed (element or PI)
	tagStart := 0
	prev := 0

	for {
		tt, _ := l.Next()
		off := in.Offset()
		if off > len(svg) {
			off = len(svg) // the work buffer carries a trailing NUL sentinel
		}
		raw := svg[prev:off:off]
		start := prev
		prev = off

		switch tt {
		case xml.ErrorToken:
			err := l.Err()
			if !errors.Is(err, io.EOF) {
				return nil, err
			}
			if open != nil {
				return nil, fmt.Errorf("dom: unterminated start tag <%s>", open.Name)
			}
			if cur != doc {
				return nil, fmt.Errorf("dom: unclosed element <%s>", cur.Name)
			}
			for _, c := range doc.Children {
				if c.Kind == KindElement {
					return doc, nil
				}
			}
			return nil, errors.New("dom: no root element")

		case xml.StartTagToken:
			open = &Node{Kind: KindElement, Name: string(l.Text())}
			tagStart = start

		case xml.StartTagPIToken:
			open = &Node{Kind: KindProcInst, Name: string(l.Text())}
			tagStart = start

		case xml.AttributeToken:
			if open != nil && open.Kind == KindElement {
				val, opaque := decodeAttrValue(l.AttrVal())
				open.Attrs = append(open.Attrs, Attr{
					Name:   string(l.Text()),
					value:  val,
					opaque: opaque,
					raw:    raw,
				})
			}

		case xml.StartTagCloseToken:
			open.rawStart = svg[tagStart:off:off]
			open.Parent = cur
			cur.Children = append(cur.Children, open)
			cur = open
			open = nil
			depth++
			if depth > maxDepth {
				return nil, errors.New("dom: nesting too deep")
			}

		case xml.StartTagCloseVoidToken:
			open.rawStart = svg[tagStart:off:off]
			open.SelfClosing = true
			open.Parent = cur
			cur.Children = append(cur.Children, open)
			open = nil

		case xml.StartTagClosePIToken:
			pi := &Node{Kind: KindProcInst, Name: open.Name, raw: svg[tagStart:off:off], Parent: cur}
			cur.Children = append(cur.Children, pi)
			open = nil

		case xml.EndTagToken:
			if cur == doc || cur.Name != string(l.Text()) {
				return nil, fmt.Errorf("dom: mismatched end tag </%s>", l.Text())
			}
			cur.rawEnd = raw
			cur = cur.Parent
			depth--

		case xml.TextToken:
			cur.Children = append(cur.Children, &Node{Kind: KindText, raw: raw, Parent: cur})

		case xml.CommentToken:
			cur.Children = append(cur.Children, &Node{Kind: KindComment, raw: raw, Parent: cur})

		case xml.CDATAToken:
			cur.Children = append(cur.Children, &Node{Kind: KindCDATA, raw: raw, Parent: cur})

		case xml.DOCTYPEToken:
			cur.Children = append(cur.Children, &Node{Kind: KindDoctype, raw: raw, Parent: cur})
		}
	}
}
