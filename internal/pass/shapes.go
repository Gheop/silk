package pass

import (
	"strconv"
	"strings"

	"github.com/Gheop/silk/internal/dom"
	"github.com/Gheop/silk/internal/path"
)

// ConvertShapes rewrites line, sharp-cornered rect, polyline and polygon
// elements as equivalent paths: the d encoding is shorter than the attribute
// form, and identical adjacent shapes then merge like any other paths. A
// stylesheet or script can address elements by type name, so either disables
// the pass; geometry with units or percentages stays as authored.
func ConvertShapes(doc *dom.Node, refs *Refs) {
	if refs.HasStylesheet {
		return
	}
	hasScript := false
	doc.Walk(func(n *dom.Node) bool {
		if n.Kind == dom.KindElement && localName(n.Name) == "script" {
			hasScript = true
		}
		return !hasScript
	})
	if hasScript {
		return
	}
	doc.Walk(func(n *dom.Node) bool {
		if n.Kind != dom.KindElement {
			return true
		}
		switch localName(n.Name) {
		case "line":
			convertLine(n)
		case "rect":
			convertRect(n)
		case "polyline":
			convertPoly(n, false)
		case "polygon":
			convertPoly(n, true)
		}
		return true
	})
}

// shapeNum parses a unitless coordinate attribute; missing means its SVG
// default of zero. Units, percentages, or anything else abort the rewrite.
func shapeNum(n *dom.Node, name string) (float64, bool) {
	if !n.HasAttr(name) {
		return 0, true
	}
	v, ok := n.AttrValue(name)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	return f, err == nil
}

func becomePath(n *dom.Node, d []byte, drop ...string) {
	for _, name := range drop {
		n.RemoveAttr(name)
	}
	n.SetAttr("d", string(d))
	n.Rename("path")
}

func convertLine(n *dom.Node) {
	x1, ok1 := shapeNum(n, "x1")
	y1, ok2 := shapeNum(n, "y1")
	x2, ok3 := shapeNum(n, "x2")
	y2, ok4 := shapeNum(n, "y2")
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return
	}
	d := append([]byte("M"), path.AppendNumberList(nil, []float64{x1, y1}, -1)...)
	d = append(d, 'L')
	d = path.AppendNumberList(d, []float64{x2, y2}, -1)
	becomePath(n, d, "x1", "y1", "x2", "y2")
}

func convertRect(n *dom.Node) {
	if n.HasAttr("rx") || n.HasAttr("ry") {
		return // rounded corners need arcs; out of scope
	}
	x, ok1 := shapeNum(n, "x")
	y, ok2 := shapeNum(n, "y")
	w, ok3 := shapeNum(n, "width")
	h, ok4 := shapeNum(n, "height")
	if !ok1 || !ok2 || !ok3 || !ok4 ||
		!n.HasAttr("width") || !n.HasAttr("height") || w <= 0 || h <= 0 {
		return
	}
	d := append([]byte("M"), path.AppendNumberList(nil, []float64{x, y}, -1)...)
	d = append(d, 'h')
	d = path.AppendNumberList(d, []float64{w}, -1)
	d = append(d, 'v')
	d = path.AppendNumberList(d, []float64{h}, -1)
	d = append(d, 'h')
	d = path.AppendNumberList(d, []float64{-w}, -1)
	d = append(d, 'z')
	becomePath(n, d, "x", "y", "width", "height")
}

func convertPoly(n *dom.Node, closed bool) {
	v, ok := n.AttrValue("points")
	if !ok {
		return
	}
	var vals []float64
	for _, f := range strings.FieldsFunc(v, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n' || r == '\r'
	}) {
		x, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return
		}
		vals = append(vals, x)
	}
	if len(vals) < 4 || len(vals)%2 != 0 {
		return
	}
	d := append([]byte("M"), path.AppendNumberList(nil, vals, -1)...)
	if closed {
		d = append(d, 'z')
	}
	becomePath(n, d, "points")
}
