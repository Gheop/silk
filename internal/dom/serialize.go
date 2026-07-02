package dom

import "bytes"

// Serialize emits the document. Nodes that were never modified are written
// from their verbatim input bytes; modified elements get a canonical start
// tag in which untouched attributes still keep their original spelling.
func Serialize(doc *Node) []byte {
	var b bytes.Buffer
	for _, c := range doc.Children {
		writeNode(&b, c)
	}
	return b.Bytes()
}

func writeNode(b *bytes.Buffer, n *Node) {
	if n.Kind != KindElement {
		b.Write(n.raw)
		return
	}
	if n.modified {
		writeStartTag(b, n)
	} else {
		b.Write(n.rawStart)
	}
	for _, c := range n.Children {
		writeNode(b, c)
	}
	if n.SelfClosing && len(n.Children) == 0 {
		return
	}
	if n.rawEnd != nil {
		b.Write(n.rawEnd)
	} else {
		b.WriteString("</")
		b.WriteString(n.Name)
		b.WriteByte('>')
	}
}

func writeStartTag(b *bytes.Buffer, n *Node) {
	b.WriteByte('<')
	b.WriteString(n.Name)
	for i := range n.Attrs {
		a := &n.Attrs[i]
		if a.raw != nil && !a.modified {
			if !n.canonical {
				b.Write(a.raw)
				continue
			}
			if a.opaque {
				// The exact spelling matters (unresolved entities): keep the
				// raw bytes, collapsing only the leading whitespace.
				b.WriteByte(' ')
				b.Write(bytes.TrimLeft(a.raw, " \t\r\n"))
				continue
			}
		}
		b.WriteByte(' ')
		b.WriteString(a.Name)
		b.WriteString(`="`)
		escapeAttrTo(b, a.value)
		b.WriteByte('"')
	}
	if n.SelfClosing && len(n.Children) == 0 {
		b.WriteString("/>")
	} else {
		b.WriteByte('>')
	}
}
