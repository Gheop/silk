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
	stripUselessXMLSpace(doc)
	removeInert(doc, refs, editorPrefixes)
	// After whitespace removal, so surviving whitespace is what it judges.
	dropRedundantXMLSpace(doc)
	stripEditorAttrsAndXmlns(doc, editorPrefixes)
	removeRedundantNamespaces(doc, refs)
	removeInertSVGAttrs(doc)
	removeUnreferencedDefs(doc, refs)
	removeEmptyContainers(doc, refs)
	doc.Walk(func(n *dom.Node) bool {
		if n.Kind == dom.KindElement {
			n.CanonicalizeStartTag()
		}
		return true
	})
}

// xmlSpaceElements render character data whose layout xml:space governs.
// <style>, <script>, <title> and <desc> carry significant text too, but CSS,
// code and tooltip strings don't change meaning with whitespace handling.
var xmlSpaceElements = map[string]bool{
	"text": true, "tspan": true, "textPath": true, "tref": true,
	"altGlyph": true, "pre": true, "foreignObject": true,
}

// stripUselessXMLSpace removes xml:space attributes from documents with no
// rendered-text content: the attribute only affects text layout, and editors
// set it on the root out of habit, blocking whitespace cleanup.
func stripUselessXMLSpace(doc *dom.Node) {
	hasText := false
	doc.Walk(func(n *dom.Node) bool {
		if n.Kind == dom.KindElement && xmlSpaceElements[localName(n.Name)] {
			hasText = true
		}
		return !hasText
	})
	if hasText {
		return
	}
	doc.Walk(func(n *dom.Node) bool {
		if n.Kind == dom.KindElement {
			n.RemoveAttr("xml:space")
		}
		return true
	})
}

// dropRedundantXMLSpace removes xml:space attributes that cannot change
// rendering: re-declarations of the inherited value, and "preserve" over
// character data so plain — no tabs, newlines, entities, leading, trailing
// or doubled spaces — that preserved and default processing render the same
// text. Editors stamp one on every text element.
func dropRedundantXMLSpace(doc *dom.Node) {
	var walk func(n *dom.Node, preserve bool)
	walk = func(n *dom.Node, preserve bool) {
		cur := preserve
		if n.Kind == dom.KindElement && n.HasAttr("xml:space") {
			switch v, ok := n.AttrValue("xml:space"); {
			case !ok:
				cur = true // opaque value: assume preserve
			case v == "default":
				if preserve {
					cur = false
				} else {
					n.RemoveAttr("xml:space")
				}
			case v == "preserve":
				cur = true
				if preserve {
					n.RemoveAttr("xml:space")
				} else if plainCharData(n) {
					n.RemoveAttr("xml:space")
					cur = false
				}
			}
		}
		for _, c := range n.Children {
			walk(c, cur)
		}
	}
	walk(doc, false)
}

// plainCharData reports whether every text node under e renders identically
// with and without xml:space="preserve". Subtrees that declare their own
// xml:space govern themselves and are skipped; anything unreadable counts
// as not plain.
func plainCharData(e *dom.Node) bool {
	plain := true
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		for _, c := range n.Children {
			if !plain {
				return
			}
			switch c.Kind {
			case dom.KindText:
				raw := c.Raw()
				if len(raw) == 0 {
					continue
				}
				if bytes.ContainsAny(raw, "\t\n\r&") ||
					raw[0] == ' ' || raw[len(raw)-1] == ' ' ||
					bytes.Contains(raw, []byte("  ")) {
					plain = false
				}
			case dom.KindElement:
				if c.HasAttr("xml:space") {
					continue // owns its own space handling
				}
				walk(c)
			case dom.KindComment, dom.KindProcInst:
				// No character data of their own.
			default:
				plain = false // CDATA and anything else: hands off
			}
		}
	}
	walk(e)
	return plain
}

// disposableDefs are element types inside <defs> that can only take effect
// through an id reference. Elements that act by name or globally (<style>,
// <font> and its face, <script>) are never candidates.
var disposableDefs = map[string]bool{
	"linearGradient": true, "radialGradient": true, "meshGradient": true,
	"pattern": true, "filter": true, "clipPath": true, "mask": true,
	"marker": true, "symbol": true, "g": true, "path": true, "rect": true,
	"circle": true, "ellipse": true, "line": true, "polyline": true,
	"polygon": true, "use": true, "image": true, "text": true,
}

// removeUnreferencedDefs drops direct children of <defs> that nothing
// references. Definitions render only through references, so an unreferenced
// one is unreachable; anything questionable (unknown types, subtrees holding
// a concretely referenced id) stays.
func removeUnreferencedDefs(doc *dom.Node, refs *Refs) {
	doc.Walk(func(n *dom.Node) bool {
		if n.Kind != dom.KindElement || localName(n.Name) != "defs" {
			return true
		}
		kept := n.Children[:0]
		for _, c := range n.Children {
			if c.Kind == dom.KindElement && disposableDefs[localName(c.Name)] &&
				!subtreeReferenced(c, refs) {
				c.Parent = nil
				continue
			}
			kept = append(kept, c)
		}
		n.Children = kept
		return true
	})
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
// something else concretely references. Callers only ask this about subtrees
// that are never rendered, so stylesheet pessimism does not apply.
func subtreeReferenced(n *dom.Node, refs *Refs) bool {
	found := false
	n.Walk(func(c *dom.Node) bool {
		if c.Kind == dom.KindElement {
			if id, ok := c.AttrValue("id"); ok && refs.ConcretelyUsedID(id) {
				found = true
				return false
			}
			if id, ok := c.AttrValue("xml:id"); ok && refs.ConcretelyUsedID(id) {
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
	preserve := false
	for e := n.Parent; e != nil && e.Kind == dom.KindElement; e = e.Parent {
		if textishElements[localName(e.Name)] {
			return false
		}
		if v, ok := e.AttrValue("xml:space"); ok && v == "preserve" {
			preserve = true
		} else if e.HasAttr("xml:space") {
			preserve = true // opaque value: assume preserve
		}
	}
	if !preserve {
		return true
	}
	// xml:space governs character data, which only text content elements
	// render; whitespace whose every ancestor provably has none is layout
	// noise even under preserve. Anything unrecognized keeps it.
	for e := n.Parent; e != nil && e.Kind == dom.KindElement; e = e.Parent {
		if !charDataFreeElements[localName(e.Name)] {
			return false
		}
	}
	return true
}

// charDataFreeElements are SVG elements whose own character data never
// renders, making whitespace-only children removable even under
// xml:space="preserve". Text containers, <style>, <script> and anything
// unknown are deliberately absent.
var charDataFreeElements = func() map[string]bool {
	m := map[string]bool{}
	for _, name := range strings.Fields(`svg g defs symbol use switch a view
		clipPath mask pattern marker image rect circle ellipse line polyline
		polygon path linearGradient radialGradient meshGradient meshrow
		meshpatch stop filter feBlend feColorMatrix feComponentTransfer
		feComposite feConvolveMatrix feDiffuseLighting feDisplacementMap
		feDistantLight feDropShadow feFlood feFuncA feFuncB feFuncG feFuncR
		feGaussianBlur feImage feMerge feMergeNode feMorphology feOffset
		fePointLight feSpecularLighting feSpotLight feTile feTurbulence
		animate animateColor animateMotion animateTransform set mpath
		font font-face font-face-src font-face-name font-face-uri
		font-face-format missing-glyph glyph glyphRef hkern vkern cursor
		color-profile`) {
		m[name] = true
	}
	return m
}()

// removeRedundantNamespaces drops namespace declarations that change
// nothing: re-declarations of a prefix already in scope with the same URI
// (some generators stamp one on every element), and declarations of prefixes
// no name in the document uses. SMIL attributeName/attributeType values can
// smuggle a prefix into an attribute value, so they count as uses; a
// stylesheet's @namespace rules can bind element selectors to any declared
// URI, so unused-prefix removal backs off entirely when one is present.
func removeRedundantNamespaces(doc *dom.Node, refs *Refs) {
	used := map[string]bool{}
	opaque := false
	doc.Walk(func(n *dom.Node) bool {
		if n.Kind != dom.KindElement {
			return true
		}
		if i := strings.IndexByte(n.Name, ':'); i > 0 {
			used[n.Name[:i]] = true
		}
		for i := range n.Attrs {
			a := &n.Attrs[i]
			if j := strings.IndexByte(a.Name, ':'); j > 0 && a.Name[:j] != "xmlns" {
				used[a.Name[:j]] = true
			}
			if a.Name == "attributeName" || a.Name == "attributeType" {
				if v, ok := a.Value(); !ok {
					opaque = true
				} else if j := strings.IndexByte(v, ':'); j > 0 {
					used[v[:j]] = true
				}
			}
		}
		return true
	})
	// CSS @namespace rules bind selector prefixes to URIs on the CSS side, so
	// a stylesheet never depends on an unused XML prefix; only a script could
	// (lookupNamespaceURI), so scripts keep declarations as they are.
	hasScript := false
	doc.Walk(func(n *dom.Node) bool {
		if n.Kind == dom.KindElement && localName(n.Name) == "script" {
			hasScript = true
		}
		return !hasScript
	})
	dropUnused := !opaque && !hasScript
	var walk func(n *dom.Node, scope map[string]string)
	walk = func(n *dom.Node, scope map[string]string) {
		next := scope
		if n.Kind == dom.KindElement {
			copied := false
			bind := func(prefix, uri string) {
				if !copied {
					m := make(map[string]string, len(next)+1)
					for k, v := range next {
						m[k] = v
					}
					next, copied = m, true
				}
				next[prefix] = uri
			}
			for i := 0; i < len(n.Attrs); {
				a := &n.Attrs[i]
				var prefix string
				switch {
				case a.Name == "xmlns":
				case strings.HasPrefix(a.Name, "xmlns:"):
					prefix = a.Name[len("xmlns:"):]
				default:
					i++
					continue
				}
				v, ok := a.Value()
				if !ok {
					// Unreadable URI: keep it, and shadow the scope so nothing
					// below is compared against a value we could not read.
					bind(prefix, "\x00")
					i++
					continue
				}
				if prefix != "" && dropUnused && !used[prefix] {
					n.RemoveAttr(a.Name)
					continue
				}
				if cur, declared := next[prefix]; declared && cur == v {
					n.RemoveAttr(a.Name)
					continue
				}
				bind(prefix, v)
				i++
			}
		}
		for _, c := range n.Children {
			walk(c, next)
		}
	}
	walk(doc, map[string]string{"": ""})
}

// removeInertSVGAttrs drops svg-element attributes that cannot affect
// rendering: version is purely informational, and x/y of zero are the
// viewport defaults.
func removeInertSVGAttrs(doc *dom.Node) {
	doc.Walk(func(n *dom.Node) bool {
		if n.Kind != dom.KindElement || localName(n.Name) != "svg" {
			return true
		}
		if _, ok := n.AttrValue("version"); ok {
			n.RemoveAttr("version")
		}
		for _, name := range [...]string{"x", "y"} {
			if v, ok := n.AttrValue(name); ok && (v == "0" || v == "0px") {
				n.RemoveAttr(name)
			}
		}
		return true
	})
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
