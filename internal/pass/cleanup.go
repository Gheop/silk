package pass

import (
	"bytes"
	"strings"

	"github.com/Gheop/silk/internal/dom"
)

// editorNamespaceURIs identifies namespaces that only carry editor state.
// Matching is by URI substring, not prefix: prefixes are conventions.
var editorNamespaceURIs = []string{
	"inkscape.org/namespaces",
	"sodipodi.sourceforge.net",
	"creativecommons.org/ns",
	"purl.org/dc/elements",
	"w3.org/1999/02/22-rdf-syntax-ns",
	"ns.adobe.com",
	"bohemiancoding.com/sketch",
	"serif.com",
	"figma.com",
	"krita.org",
	"vectornator.io",
	"taptrix.com/vectorillustrator",
}

// Cleanup removes constructs that cannot affect rendering: comments,
// metadata, editor-namespace elements and attributes (with their xmlns
// declarations once unused), the XML prolog and doctype when provably inert,
// insignificant whitespace, and empty containers.
func Cleanup(doc *dom.Node, refs *Refs) {
	editorPrefixes := collectEditorPrefixes(doc)
	removeInert(doc, refs, editorPrefixes)
	stripEditorAttrsAndXmlns(doc, editorPrefixes)
	removeEmptyContainers(doc, refs)
}

// collectEditorPrefixes maps namespace prefixes bound to editor URIs.
func collectEditorPrefixes(doc *dom.Node) map[string]bool {
	prefixes := map[string]bool{}
	doc.Walk(func(n *dom.Node) bool {
		if n.Kind != dom.KindElement {
			return true
		}
		for i := range n.Attrs {
			a := &n.Attrs[i]
			if !strings.HasPrefix(a.Name, "xmlns:") {
				continue
			}
			v, ok := a.Value()
			if !ok {
				continue
			}
			for _, uri := range editorNamespaceURIs {
				if strings.Contains(v, uri) {
					prefixes[a.Name[len("xmlns:"):]] = true
					break
				}
			}
		}
		return true
	})
	return prefixes
}

func removeInert(doc *dom.Node, refs *Refs, editorPrefixes map[string]bool) {
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		kept := n.Children[:0]
		for _, c := range n.Children {
			if removableInert(c, refs, editorPrefixes) {
				c.Parent = nil
				continue
			}
			walk(c)
			kept = append(kept, c)
		}
		n.Children = kept
	}
	walk(doc)
}

func removableInert(n *dom.Node, refs *Refs, editorPrefixes map[string]bool) bool {
	switch n.Kind {
	case dom.KindComment:
		return true
	case dom.KindDoctype:
		// An internal subset can declare entities the document uses.
		return !bytes.ContainsRune(n.Raw(), '[')
	case dom.KindProcInst:
		// The prolog is inert unless it pins a non-UTF-8 encoding.
		if n.Name != "xml" {
			return false
		}
		raw := strings.ToLower(string(n.Raw()))
		if !strings.Contains(raw, "encoding") {
			return true
		}
		return strings.Contains(raw, "utf-8") || strings.Contains(raw, "us-ascii")
	case dom.KindText:
		return insignificantWhitespace(n)
	case dom.KindElement:
		if localName(n.Name) == "metadata" {
			return !subtreeReferenced(n, refs)
		}
		if i := strings.IndexByte(n.Name, ':'); i > 0 && editorPrefixes[n.Name[:i]] {
			return !subtreeReferenced(n, refs)
		}
	}
	return false
}

// subtreeReferenced reports whether any element inside n carries an id that
// something else references.
func subtreeReferenced(n *dom.Node, refs *Refs) bool {
	found := false
	n.Walk(func(c *dom.Node) bool {
		if c.Kind == dom.KindElement {
			if id, ok := c.AttrValue("id"); ok && refs.UsedID(id) {
				found = true
				return false
			}
		}
		return !found
	})
	return found
}

// textishElements are elements whose text content is significant.
var textishElements = map[string]bool{
	"text": true, "tspan": true, "textPath": true, "tref": true,
	"style": true, "script": true, "title": true, "desc": true,
	"pre": true, "foreignObject": true,
}

func insignificantWhitespace(n *dom.Node) bool {
	if len(bytes.TrimLeft(n.Raw(), " \t\r\n")) > 0 {
		return false
	}
	// A UTF-8 BOM or other bytes: TrimLeft above only strips whitespace, so
	// reaching here means pure whitespace.
	for e := n.Parent; e != nil && e.Kind == dom.KindElement; e = e.Parent {
		if textishElements[localName(e.Name)] {
			return false
		}
		if v, ok := e.AttrValue("xml:space"); ok && v == "preserve" {
			return false
		}
		if e.HasAttr("xml:space") {
			if _, ok := e.AttrValue("xml:space"); !ok {
				return false // opaque value: assume preserve
			}
		}
	}
	return true
}

func stripEditorAttrsAndXmlns(doc *dom.Node, editorPrefixes map[string]bool) {
	if len(editorPrefixes) == 0 {
		return
	}
	// First drop editor-prefixed attributes everywhere.
	doc.Walk(func(n *dom.Node) bool {
		if n.Kind != dom.KindElement {
			return true
		}
		for i := 0; i < len(n.Attrs); {
			name := n.Attrs[i].Name
			if j := strings.IndexByte(name, ':'); j > 0 && name[:j] != "xmlns" && name[:j] != "xml" && editorPrefixes[name[:j]] {
				n.RemoveAttr(name)
				continue
			}
			i++
		}
		return true
	})
	// Then drop xmlns declarations whose prefix no longer appears.
	stillUsed := map[string]bool{}
	doc.Walk(func(n *dom.Node) bool {
		if n.Kind != dom.KindElement {
			return true
		}
		if i := strings.IndexByte(n.Name, ':'); i > 0 {
			stillUsed[n.Name[:i]] = true
		}
		for i := range n.Attrs {
			name := n.Attrs[i].Name
			if j := strings.IndexByte(name, ':'); j > 0 && name[:j] != "xmlns" {
				stillUsed[name[:j]] = true
			}
		}
		return true
	})
	doc.Walk(func(n *dom.Node) bool {
		if n.Kind != dom.KindElement {
			return true
		}
		for prefix := range editorPrefixes {
			if !stillUsed[prefix] {
				n.RemoveAttr("xmlns:" + prefix)
			}
		}
		return true
	})
}

// emptiableContainers can be dropped when they have no children: they render
// nothing by themselves.
var emptiableContainers = map[string]bool{"g": true, "defs": true}

func removeEmptyContainers(doc *dom.Node, refs *Refs) {
	for {
		removed := false
		var walk func(n *dom.Node)
		walk = func(n *dom.Node) {
			kept := n.Children[:0]
			for _, c := range n.Children {
				walk(c)
				if emptyContainer(c, refs) {
					c.Parent = nil
					removed = true
					continue
				}
				kept = append(kept, c)
			}
			n.Children = kept
		}
		walk(doc)
		if !removed {
			return
		}
	}
}

func emptyContainer(n *dom.Node, refs *Refs) bool {
	if n.Kind != dom.KindElement || !emptiableContainers[localName(n.Name)] || len(n.Children) > 0 {
		return false
	}
	if id, ok := n.AttrValue("id"); ok && refs.UsedID(id) {
		return false
	}
	if refs.HasStylesheet {
		return false
	}
	// A filter can paint even over empty content (e.g. feFlood with an
	// explicit region), and any url() reference is a dependency we keep.
	for i := range n.Attrs {
		v, ok := n.Attrs[i].Value()
		if !ok || strings.Contains(v, "url(") {
			return false
		}
	}
	return true
}
