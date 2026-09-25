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
	if refs.Dynamic() {
		return
	}
	// Unwrapping is decided bottom-up and applied per parent by rebuilding
	// its child list once, so a document with n sibling groups costs O(n)
	// rather than O(n²) of splices. A pass can enable more collapses above
	// (a group left with a lone child), hence the fixed-point loop.
	for collapseChildren(doc, refs) {
	}
}

// collapseChildren unwraps the collapsible groups among p's descendants,
// deepest first, and reports whether anything changed.
func collapseChildren(p *dom.Node, refs *Refs) bool {
	changed := false
	for _, c := range p.Children {
		if c.Kind == dom.KindElement && len(c.Children) > 0 && collapseChildren(c, refs) {
			changed = true
		}
	}
	unwrap := 0
	for _, c := range p.Children {
		if c.Kind == dom.KindElement && collapseGroup(c, refs) {
			unwrap++
		}
	}
	if unwrap == 0 {
		return changed
	}
	kept := make([]*dom.Node, 0, len(p.Children)+unwrap)
	for _, c := range p.Children {
		if c.Kind == dom.KindElement && c.Parent == nil {
			// Marked by collapseGroup: its children take its place.
			for _, gc := range c.Children {
				gc.Parent = p
			}
			kept = append(kept, c.Children...)
			c.Children = nil
			continue
		}
		kept = append(kept, c)
	}
	p.Children = kept
	return true
}

// collapseGroup decides whether g unwraps, pushes its attributes onto its
// lone child when it does, and marks it (Parent cleared) for the caller to
// splice its children in place.
func collapseGroup(g *dom.Node, refs *Refs) bool {
	if localName(g.Name) != "g" || g.Parent == nil || underSwitch(g) {
		return false
	}
	if g.HasAttr("id") {
		return false // even unreferenced: dropping ids is not this pass's call
	}
	// clipPath admits only shapes, text and use: renderers ignore a <g>
	// there, so unwrapping it would turn an empty clip into a visible one.
	if localName(g.Parent.Name) == "clipPath" {
		return false
	}
	if len(g.Attrs) == 0 {
		g.Parent = nil
		return true
	}
	child := loneElementChild(g)
	if child == nil || child.HasAttr("style") {
		return false
	}
	// A nested svg establishes its own viewport and SVG 1.1 renderers
	// ignore a transform on it (and on symbol, clipPath, mask, pattern,
	// marker): the group's attributes would land where they no longer apply.
	switch localName(child.Name) {
	case "svg", "symbol", "clipPath", "mask", "pattern", "marker":
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
			// transform-origin/box apply to the element's own transform: on
			// a child without one they are inert, and pushing the group's
			// transform down would activate them.
			if child.HasAttr("transform-origin") || child.HasAttr("transform-box") {
				return false
			}
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
	g.Parent = nil
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
