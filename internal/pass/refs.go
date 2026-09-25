package pass

import (
	"regexp"
	"strings"

	"github.com/Gheop/silk/internal/dom"
)

// Refs is the reference-safety graph: everything an optimization pass must
// treat as untouchable. It over-approximates on purpose — a reference we
// cannot fully resolve marks its target as used.
type Refs struct {
	// IDs referenced by url(#...), href, aria attributes, or stylesheet text.
	ids map[string]bool

	// HasStylesheet is set when a <style> element or an xml-stylesheet
	// processing instruction exists. Selectors can restyle or re-target
	// arbitrary elements, so structural passes that remove or merge elements
	// are disabled wholesale.
	HasStylesheet bool

	// HasUse is set when any <use> exists: a subtree can then be re-rendered
	// under different inherited properties.
	HasUse bool

	// HasScript is set when a <script> element or an event handler
	// attribute (onclick, onload...) exists: code can address any element
	// by id, read attributes and restructure the tree, so structural passes
	// are disabled like under a stylesheet.
	HasScript bool

	// HasAnimation is set when a SMIL animation element exists: attribute
	// values then change over time, so anything decided from a static
	// value (a stroke of none, an inherited default) may not hold.
	HasAnimation bool
}

// animationElements are the SMIL elements whose attributes carry values
// (possibly id references) that only apply over time.
var animationElements = map[string]bool{
	"animate": true, "set": true, "animateTransform": true,
	"animateMotion": true, "animateColor": true,
}

// Dynamic reports that something outside the markup (a stylesheet, a
// script) can re-target or restyle arbitrary elements: passes that remove,
// merge or restructure elements must stand down.
func (r *Refs) Dynamic() bool {
	return r.HasStylesheet || r.HasScript
}

// UsedID reports whether the id may be referenced from anywhere.
func (r *Refs) UsedID(id string) bool {
	return r.Dynamic() || r.ids[id]
}

// ConcretelyUsedID reports whether the id is actually referenced (by url(),
// href, aria, or a #id token in stylesheet text). Unlike UsedID it does not
// pessimize on the mere presence of a stylesheet: never-rendered subtrees
// (metadata, editor namespaces) cannot be made visible by CSS, so only real
// references protect them.
func (r *Refs) ConcretelyUsedID(id string) bool {
	return r.ids[id]
}

var urlRefPattern = regexp.MustCompile(`url\(\s*['"]?#([^'")]+)['"]?\s*\)`)

// CSS identifiers admit any non-ASCII character; \w would stop at the first
// multibyte rune and record a truncated id that matches nothing.
var cssHashPattern = regexp.MustCompile(`#([\pL_][\pL\pN_.-]*)`)

// Analyze builds the reference graph for a document.
func Analyze(doc *dom.Node) *Refs {
	r := &Refs{ids: map[string]bool{}}
	doc.Walk(func(n *dom.Node) bool {
		switch n.Kind {
		case dom.KindProcInst:
			if n.Name == "xml-stylesheet" {
				r.HasStylesheet = true
			}
			return true
		case dom.KindElement:
		default:
			return true
		}
		switch localName(n.Name) {
		case "style":
			r.HasStylesheet = true
			for _, c := range n.Children {
				for _, m := range cssHashPattern.FindAllSubmatch(c.Raw(), -1) {
					r.ids[string(m[1])] = true
				}
			}
		case "use":
			r.HasUse = true
		case "script":
			r.HasScript = true
		}
		animation := animationElements[localName(n.Name)]
		if animation {
			r.HasAnimation = true
		}
		for i := range n.Attrs {
			a := &n.Attrs[i]
			if strings.HasPrefix(a.Name, "on") {
				r.HasScript = true
			}
			if animation {
				// An animated href or paint takes each of its values in
				// turn: every #id token is a reference.
				switch a.Name {
				case "from", "to", "by", "values":
					v, _ := a.Value()
					for _, tok := range strings.FieldsFunc(v, func(c rune) bool { return c == ';' || c == ' ' || c == '\t' || c == '\n' }) {
						if len(tok) > 1 && tok[0] == '#' {
							r.ids[tok[1:]] = true
						}
					}
				}
			}
			// The value is scanned even when it did not decode cleanly: an
			// opaque value must still pin whatever it might reference.
			v, _ := a.Value()
			switch a.Name {
			case "href", "xlink:href":
				// URLs resolve after whitespace trimming.
				if v := strings.TrimSpace(v); len(v) > 1 && v[0] == '#' {
					r.ids[v[1:]] = true
				}
			case "aria-labelledby", "aria-describedby":
				for _, id := range strings.Fields(v) {
					r.ids[id] = true
				}
			}
			if strings.Contains(v, "url(") {
				for _, m := range urlRefPattern.FindAllStringSubmatch(v, -1) {
					r.ids[m[1]] = true
				}
			}
		}
		return true
	})
	return r
}
