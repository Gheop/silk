// Package dom holds a raw-preserving SVG document tree. Every node keeps the
// exact input bytes that produced it; serialization emits those bytes verbatim
// unless the node was modified. Untouched markup therefore round-trips
// byte-for-byte, which is what makes "when unsure, leave it unchanged" safe
// and the output deterministic.
package dom

import "bytes"

// Kind discriminates node types in the tree.
type Kind uint8

const (
	KindDocument Kind = iota
	KindElement
	KindText
	KindComment
	KindCDATA
	KindDoctype
	KindProcInst
)

// Attr is one attribute of an element. The raw form (leading whitespace,
// original quoting, undecoded entities) is kept so that untouched attributes
// serialize verbatim even when a sibling attribute changed.
type Attr struct {
	// Name is the attribute name as written, including any namespace prefix.
	Name string

	value    string // decoded value
	opaque   bool   // value contains constructs we could not decode (e.g. DTD entities)
	raw      []byte // verbatim input bytes: leading whitespace + name + "=" + quoted value
	modified bool
}

// Value returns the decoded attribute value. ok is false when the value could
// not be cleanly decoded (unknown entity); such values must be treated as
// opaque and never rewritten.
func (a *Attr) Value() (string, bool) {
	return a.value, !a.opaque
}

// Node is a document, element, or leaf (text, comment, CDATA, doctype, PI).
type Node struct {
	Kind     Kind
	Name     string // element or PI name as written, including prefix
	Attrs    []Attr
	Children []*Node
	Parent   *Node

	// SelfClosing records whether the element used <name/> form.
	SelfClosing bool

	raw       []byte // leaf kinds: the complete verbatim token
	rawStart  []byte // element: verbatim start tag, "<name ...>" or "<name.../>"
	rawEnd    []byte // element: verbatim end tag, nil when self-closing
	modified  bool   // start tag must be re-serialized canonically
	canonical bool   // canonical re-serialization also normalizes attr spacing
}

// Raw returns the verbatim input bytes of a leaf node (text, comment, CDATA,
// doctype, processing instruction). It is nil for elements and documents.
func (n *Node) Raw() []byte { return n.raw }

// AttrValue returns the decoded value of the named attribute. ok is false when
// the attribute is absent or its value is opaque.
func (n *Node) AttrValue(name string) (string, bool) {
	for i := range n.Attrs {
		if n.Attrs[i].Name == name {
			return n.Attrs[i].Value()
		}
	}
	return "", false
}

// HasAttr reports whether the named attribute is present.
func (n *Node) HasAttr(name string) bool {
	for i := range n.Attrs {
		if n.Attrs[i].Name == name {
			return true
		}
	}
	return false
}

// SetAttr sets the named attribute, appending it when absent, and marks the
// element for canonical re-serialization of its start tag.
func (n *Node) SetAttr(name, value string) {
	n.modified = true
	for i := range n.Attrs {
		if n.Attrs[i].Name == name {
			n.Attrs[i].value = value
			n.Attrs[i].opaque = false
			n.Attrs[i].modified = true
			return
		}
	}
	n.Attrs = append(n.Attrs, Attr{Name: name, value: value, modified: true})
}

// RemoveAttr removes the named attribute if present.
func (n *Node) RemoveAttr(name string) {
	for i := range n.Attrs {
		if n.Attrs[i].Name == name {
			n.Attrs = append(n.Attrs[:i], n.Attrs[i+1:]...)
			n.modified = true
			return
		}
	}
}

// RemoveChild removes c from n's children. Surrounding markup is unaffected.
func (n *Node) RemoveChild(c *Node) {
	for i, ch := range n.Children {
		if ch == c {
			n.Children = append(n.Children[:i], n.Children[i+1:]...)
			c.Parent = nil
			return
		}
	}
}

// ReplaceWithChildren splices n's children into its parent in n's place,
// dropping n's own tags. Used to unwrap groups.
func (n *Node) ReplaceWithChildren() {
	p := n.Parent
	if p == nil {
		return
	}
	for i, ch := range p.Children {
		if ch == n {
			rest := append([]*Node(nil), p.Children[i+1:]...)
			p.Children = append(p.Children[:i], n.Children...)
			p.Children = append(p.Children, rest...)
			for _, c := range n.Children {
				c.Parent = p
			}
			n.Parent = nil
			n.Children = nil
			return
		}
	}
}

// CanonicalizeStartTag re-serializes the start tag with single-space
// attribute separation when the original spelling wastes bytes (editor
// indentation inside tags). Inter-attribute whitespace is not significant in
// XML, so the document is equivalent.
func (n *Node) CanonicalizeStartTag() {
	if n.Kind != KindElement || n.modified {
		return
	}
	for i := range n.Attrs {
		raw := n.Attrs[i].raw
		if len(raw) == 0 {
			continue
		}
		wasted := raw[0] == '\n' || raw[0] == '\t' || raw[0] == '\r' ||
			(len(raw) > 1 && raw[0] == ' ' && (raw[1] == ' ' || raw[1] == '\t' || raw[1] == '\n' || raw[1] == '\r'))
		if !wasted {
			for j := 1; j < len(raw); j++ {
				if raw[j] == '\n' || raw[j] == '\t' || raw[j] == '\r' {
					wasted = true
					break
				}
			}
		}
		if wasted {
			n.modified = true
			n.canonical = true
			return
		}
	}
	// The tag itself may hold whitespace before '>' or between name and the
	// first attribute even with no attributes at all.
	if bytes.ContainsAny(n.rawStart, "\n\t\r") {
		n.modified = true
		n.canonical = true
	}
}

// Walk visits n and its descendants in document order. Returning false from
// fn skips the node's children.
func (n *Node) Walk(fn func(*Node) bool) {
	if n.Kind != KindDocument {
		if !fn(n) {
			return
		}
	}
	for _, c := range n.Children {
		c.Walk(fn)
	}
}
