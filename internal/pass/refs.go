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
}

// UsedID reports whether the id may be referenced from anywhere.
func (r *Refs) UsedID(id string) bool {
	return r.HasStylesheet || r.ids[id]
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
var cssHashPattern = regexp.MustCompile(`#([A-Za-z_][\w.-]*)`)

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
		}
		for i := range n.Attrs {
			a := &n.Attrs[i]
			// The value is scanned even when it did not decode cleanly: an
			// opaque value must still pin whatever it might reference.
			v, _ := a.Value()
			switch a.Name {
			case "href", "xlink:href":
				if len(v) > 1 && v[0] == '#' {
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
