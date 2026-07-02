// Package pass hosts the document-level optimization passes. Each pass is an
// isolated function over the dom tree; anything it cannot prove safe it
// leaves byte-untouched.
package pass

import (
	"strings"

	"github.com/Gheop/silk/internal/dom"
	"github.com/Gheop/silk/internal/path"
)

// OptimizePaths rewrites every d attribute whose optimized encoding is
// strictly shorter. Unparseable path data is left as found.
func OptimizePaths(doc *dom.Node, prec int) {
	docSafe := noopSafeDoc(doc)
	doc.Walk(func(n *dom.Node) bool {
		if n.Kind != dom.KindElement || localName(n.Name) != "path" {
			return true
		}
		d, ok := n.AttrValue("d")
		if !ok {
			return true
		}
		cs, err := path.Parse([]byte(d))
		if err != nil {
			return true
		}
		p := prec
		noops := docSafe && noopSafeElement(n)
		if underFilter(n) {
			// A filter's region and primitives (feTurbulence in particular)
			// sample relative to the exact geometry; any coordinate change
			// can shift what they produce. Re-encode losslessly only.
			p = -1
			noops = false
		}
		out := path.Optimize(cs, path.Options{
			Precision:   p,
			RemoveNoops: noops,
		})
		// nil means no stable encoding was found; an empty result is only
		// valid when the input path was itself empty of any drawing.
		if out != nil && len(out) > 0 && len(out) < len(d) {
			n.SetAttr("d", string(out))
		}
		return true
	})
}

func localName(name string) string {
	if i := strings.IndexByte(name, ':'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// noopSafeDoc reports whether zero-length segments can be dropped anywhere in
// the document. A stylesheet could set stroke or markers through selectors we
// do not resolve, and <use> can re-render a path under different inherited
// properties, so either disables removal wholesale.
func noopSafeDoc(doc *dom.Node) bool {
	safe := true
	doc.Walk(func(n *dom.Node) bool {
		switch {
		case n.Kind == dom.KindElement && localName(n.Name) == "style",
			n.Kind == dom.KindElement && localName(n.Name) == "use",
			n.Kind == dom.KindProcInst && n.Name == "xml-stylesheet":
			safe = false
			return false
		}
		return safe
	})
	return safe
}

// underFilter reports whether a filter applies to the element or any of its
// ancestors.
func underFilter(n *dom.Node) bool {
	for e := n; e != nil && e.Kind == dom.KindElement; e = e.Parent {
		if e.HasAttr("filter") {
			return true
		}
		if e.HasAttr("style") {
			if v, ok := e.AttrValue("style"); !ok || strings.Contains(v, "filter") {
				return true
			}
		}
	}
	return false
}

// noopSafeElement reports whether the element is provably unstroked and
// unmarkered: zero-length segments render nothing only then.
func noopSafeElement(n *dom.Node) bool {
	strokeResolved := false
	for e := n; e != nil && e.Kind == dom.KindElement; e = e.Parent {
		for i := range e.Attrs {
			a := &e.Attrs[i]
			v, ok := a.Value()
			switch a.Name {
			case "stroke":
				if !strokeResolved {
					if !ok || strings.TrimSpace(v) != "none" {
						return false
					}
					strokeResolved = true
				}
			case "style":
				// Rough but conservative: any mention of stroke or marker in
				// inline CSS blocks removal.
				if !ok || strings.Contains(v, "stroke") || strings.Contains(v, "marker") {
					return false
				}
			case "marker", "marker-start", "marker-mid", "marker-end":
				return false
			}
		}
	}
	return true
}
