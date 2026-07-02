package pass

import (
	"github.com/Gheop/silk/internal/dom"
)

// pushableAttrs are attributes that may move from a group onto a lone child
// with identical rendering: inheritable presentation attributes, plus
// transform (concatenated) and opacity (equivalent for a single child).
var pushableAttrs = map[string]bool{
	"color": true, "fill": true, "fill-opacity": true, "fill-rule": true,
	"stroke": true, "stroke-width": true, "stroke-linecap": true,
	"stroke-linejoin": true, "stroke-miterlimit": true,
	"stroke-dasharray": true, "stroke-dashoffset": true, "stroke-opacity": true,
	"font-family": true, "font-size": true, "font-style": true,
	"font-weight": true, "font-variant": true, "font-stretch": true,
	"letter-spacing": true, "word-spacing": true, "text-anchor": true,
	"visibility": true, "shape-rendering": true, "text-rendering": true,
	"image-rendering": true, "color-interpolation": true,
	"color-interpolation-filters": true, "paint-order": true,
	"transform": true, "opacity": true,
}

// CollapseGroups unwraps groups that provably change nothing: groups with no
// attributes, and groups whose few attributes can move onto their only
// child. Anything a stylesheet could target, anything referenced, and any
// group carrying clipping, masking, filtering, or inline CSS is left alone.
func CollapseGroups(doc *dom.Node, refs *Refs) {
	if refs.HasStylesheet {
		return
	}
	for {
		changed := false
		doc.Walk(func(n *dom.Node) bool {
			if n.Kind == dom.KindElement && collapseGroup(n, refs) {
				changed = true
			}
			return true
		})
		if !changed {
			return
		}
	}
}

func collapseGroup(g *dom.Node, refs *Refs) bool {
	if localName(g.Name) != "g" || g.Parent == nil {
		return false
	}
	if g.HasAttr("id") {
		return false // even unreferenced: dropping ids is not this pass's call
	}
	if len(g.Attrs) == 0 {
		g.ReplaceWithChildren()
		return true
	}
	child := loneElementChild(g)
	if child == nil || child.HasAttr("style") {
		return false
	}
	if id, ok := child.AttrValue("id"); ok && refs.UsedID(id) {
		// The child renders differently through <use> once attributes land
		// on it directly.
		return false
	}
	// Every group attribute must be movable, and the move must be computed
	// before mutating anything.
	type move struct{ name, value string }
	var moves []move
	for i := range g.Attrs {
		a := &g.Attrs[i]
		if !pushableAttrs[a.Name] {
			return false
		}
		v, ok := a.Value()
		if !ok {
			return false
		}
		switch {
		case a.Name == "transform":
			if cv, cok := child.AttrValue("transform"); child.HasAttr("transform") {
				if !cok {
					return false
				}
				moves = append(moves, move{"transform", v + " " + cv})
			} else {
				moves = append(moves, move{"transform", v})
			}
		case child.HasAttr(a.Name):
			cv, cok := child.AttrValue(a.Name)
			if !cok || cv == "inherit" {
				return false
			}
			if a.Name == "opacity" {
				return false // combining opacities is a numeric rewrite; skip
			}
			// The child's own value already masks the group's: drop it.
		default:
			moves = append(moves, move{a.Name, v})
		}
	}
	for _, m := range moves {
		child.SetAttr(m.name, m.value)
	}
	g.ReplaceWithChildren()
	return true
}

func loneElementChild(g *dom.Node) *dom.Node {
	var el *dom.Node
	for _, c := range g.Children {
		switch c.Kind {
		case dom.KindElement:
			if el != nil {
				return nil
			}
			el = c
		case dom.KindComment:
		default:
			return nil // text or CDATA between tags: leave the group alone
		}
	}
	return el
}
