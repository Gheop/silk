package dom

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"unicode/utf16"
	"unicode/utf8"

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
	// UTF-16 documents (CorelDRAW exports, some Windows tools) transcode to
	// UTF-8 up front; the whole tree, raw slices included, then lives in
	// UTF-8 and serializes as such. The prolog's encoding declaration is
	// rewritten to match, so the output document stays self-consistent.
	if u8 := decodeUTF16(svg); u8 != nil {
		doc, err := Parse(u8)
		if err != nil {
			return nil, err
		}
		fixEncodingDecl(doc)
		return doc, nil
	}
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
	var entities map[string]string
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
				val, opaque := decodeAttrValue(l.AttrVal(), entities)
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
			entities = parseInternalSubset(raw)
		}
	}
}

// decodeUTF16 returns the UTF-8 transcoding of a UTF-16 document, or nil
// when the input is not UTF-16. Detection: byte-order mark, or a null byte
// interleaved with the leading '<' (XML must start with '<' or whitespace,
// so an early NUL only occurs as the high byte of a UTF-16 code unit).
func decodeUTF16(b []byte) []byte {
	if len(b) < 4 || len(b)%2 != 0 {
		return nil
	}
	le := false
	switch {
	case b[0] == 0xFF && b[1] == 0xFE:
		le = true
		b = b[2:]
	case b[0] == 0xFE && b[1] == 0xFF:
		b = b[2:]
	case b[0] == '<' && b[1] == 0x00:
		le = true
	case b[0] == 0x00 && b[1] == '<':
	default:
		return nil
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		if le {
			units[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		} else {
			units[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		}
	}
	out := make([]byte, 0, len(b))
	for _, r := range utf16.Decode(units) {
		out = utf8.AppendRune(out, r)
	}
	return out
}

var encodingDeclPattern = regexp.MustCompile(`(?i)encoding\s*=\s*(['"])[^'"]*(['"])`)

// fixEncodingDecl rewrites a non-UTF-8 encoding declaration in the xml
// prolog to utf-8, matching the transcoded bytes.
func fixEncodingDecl(doc *Node) {
	for _, c := range doc.Children {
		if c.Kind != KindProcInst || c.Name != "xml" {
			continue
		}
		raw := string(c.Raw())
		if out := encodingDeclPattern.ReplaceAllString(raw, "encoding=${1}UTF-8${2}"); out != raw {
			c.SetText(out)
		}
		return
	}
}
