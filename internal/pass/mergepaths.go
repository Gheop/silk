package pass

import (
	"math"
	"strconv"
	"strings"

	"github.com/Gheop/silk/internal/dom"
	"github.com/Gheop/silk/internal/path"
)

// MergePaths joins adjacent <path> siblings that share every attribute and
// provably paint independently. Merging reorders painting (all fill, then
// all stroke, in one element), so overlap is only tolerated when nothing can
// show the difference: opaque fill, nonzero winding, no stroke. Otherwise
// the bounding boxes — inflated by the stroke reach — must be disjoint.
func MergePaths(doc *dom.Node, refs *Refs, prec int, cache *PathCache) {
	if refs.HasStylesheet {
		return
	}
	m := merger{refs: refs, prec: prec, docSafe: noopSafeDoc(doc), cache: cache}
	doc.Walk(func(n *dom.Node) bool {
		if n.Kind != dom.KindElement && n.Kind != dom.KindDocument {
			return true
		}
		m.mergeChildren(n)
		return true
	})
}

type merger struct {
	refs    *Refs
	prec    int
	docSafe bool
	cache   *PathCache
}

func (m *merger) mergeChildren(parent *dom.Node) {
	i := 0
	// accA carries the merged geometry's box across a chain of merges, so a
	// chain costs one box per member instead of re-measuring the growing
	// result each step.
	var accA *bbox
	for i < len(parent.Children)-1 {
		a, b := parent.Children[i], parent.Children[i+1]
		if merged, union := m.tryMerge(a, b, accA); merged {
			parent.RemoveChild(b)
			accA = union
			continue // a may swallow the next sibling too
		}
		accA = nil
		i++
	}
}

// emittedCmds predicts the geometry the path pass will emit for this
// element. Merge decisions taken on the raw input would flip on the second
// run, when the input is the rounded output; deciding on the emitted
// geometry keeps them stable.
func (m *merger) emittedCmds(el *dom.Node, d string, cs []path.Cmd) []path.Cmd {
	p, noops := pathOptions(el, m.prec, m.docSafe)
	out, ok := m.cache.optimize(d, p, noops)
	if !ok {
		return cs
	}
	parsed, err := path.Parse([]byte(out))
	if err != nil {
		return cs
	}
	return parsed
}

func (m *merger) tryMerge(a, b *dom.Node, accA *bbox) (bool, *bbox) {
	if a.Kind != dom.KindElement || b.Kind != dom.KindElement ||
		localName(a.Name) != "path" || localName(b.Name) != "path" {
		return false, nil
	}
	if a.HasAttr("id") || b.HasAttr("id") {
		return false, nil
	}
	if !sameAttrsExceptD(a, b) {
		return false, nil
	}
	da, okA := a.AttrValue("d")
	db, okB := b.AttrValue("d")
	if !okA || !okB {
		return false, nil
	}
	csA, errA := path.Parse([]byte(da))
	csB, errB := path.Parse([]byte(db))
	if errA != nil || errB != nil || len(csA) == 0 || len(csB) == 0 {
		return false, nil
	}
	inflate, ok := strokeReach(a)
	if !ok {
		return false, nil
	}
	ba := accA
	if ba == nil {
		if box, bok := m.emittedBBox(a, da, csA); bok {
			ba = &box
		}
	}
	bb, okBB := m.emittedBBox(b, db, csB)
	if ba == nil || !okBB || !disjoint(*ba, bb, inflate) {
		return false, nil
	}
	// A leading lowercase moveto is absolute by definition at the start of
	// path data; after concatenation it must say so explicitly.
	if csB[0].Op == 'm' {
		csB[0].Op = 'M'
	}
	merged := path.Serialize(nil, append(csA, csB...), -1)
	if _, err := path.Parse(merged); err != nil {
		return false, nil
	}
	a.SetAttr("d", string(merged))
	union := bbox{
		min(ba.minX, bb.minX), min(ba.minY, bb.minY),
		max(ba.maxX, bb.maxX), max(ba.maxY, bb.maxY),
	}
	return true, &union
}

// emittedBBox measures the geometry the path pass will emit for d.
func (m *merger) emittedBBox(el *dom.Node, d string, cs []path.Cmd) (bbox, bool) {
	return controlBBox(m.emittedCmds(el, d, cs))
}

// blockedAttrs on either path always prevent merging: they depend on the
// element's own geometry or identity.
func blockedAttr(name, value string) bool {
	if strings.HasPrefix(name, "marker") || name == "pathLength" || name == "style" {
		return true
	}
	// Bounding-box-relative units make gradients, patterns, clips and masks
	// resolve differently against the merged geometry.
	return strings.Contains(value, "url(")
}

func sameAttrsExceptD(a, b *dom.Node) bool {
	countA := 0
	for i := range a.Attrs {
		name := a.Attrs[i].Name
		if name == "d" {
			continue
		}
		countA++
		va, okA := a.Attrs[i].Value()
		vb, okB := b.AttrValue(name)
		if !okA || !okB || va != vb || !b.HasAttr(name) || blockedAttr(name, va) {
			return false
		}
	}
	countB := 0
	for i := range b.Attrs {
		if b.Attrs[i].Name != "d" {
			countB++
		}
	}
	return countA == countB
}

// strokeReach returns how far painting can extend beyond the geometry's
// bounding box. Merging is only safe for provably disjoint geometry: overlap
// cancels winding under the nonzero rule (and punches holes under evenodd),
// re-orders strokes against fills, and double-paints partial opacity.
func strokeReach(a *dom.Node) (float64, bool) {
	stroke, strokeW, ok := strokeInfo(a)
	if !ok {
		return 0, false
	}
	if !stroke {
		return 0, true
	}
	// Covers the stroke body plus the default miter reach (limit 4).
	limit := 4.0
	if v, lok := a.AttrValue("stroke-miterlimit"); lok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && f > limit {
			limit = f
		}
	}
	return strokeW / 2 * limit, true
}

// strokeInfo resolves whether the element can be stroked and with what
// width, walking ancestors for the nearest values. ok is false when inline
// CSS or unparseable values make the answer unknowable.
func strokeInfo(n *dom.Node) (stroked bool, width float64, ok bool) {
	width = 1 // SVG default stroke-width
	strokeSeen, widthSeen := false, false
	for e := n; e != nil && e.Kind == dom.KindElement; e = e.Parent {
		for i := range e.Attrs {
			a := &e.Attrs[i]
			v, vok := a.Value()
			switch a.Name {
			case "stroke":
				if !strokeSeen {
					if !vok {
						return false, 0, false
					}
					strokeSeen = true
					stroked = strings.TrimSpace(v) != "none"
				}
			case "stroke-width":
				if !widthSeen {
					if !vok {
						return false, 0, false
					}
					f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
					if err != nil || f < 0 {
						return false, 0, false
					}
					widthSeen = true
					width = f
				}
			case "style":
				if !vok || strings.Contains(v, "stroke") {
					return false, 0, false
				}
			}
		}
	}
	return stroked, width, true
}

type bbox struct{ minX, minY, maxX, maxY float64 }

func disjoint(a, b bbox, inflate float64) bool {
	return a.maxX+inflate < b.minX-inflate || b.maxX+inflate < a.minX-inflate ||
		a.maxY+inflate < b.minY-inflate || b.maxY+inflate < a.minY-inflate
}

// controlBBox computes a conservative bounding box from the absolute control
// points: curves lie within their control hull (reflected controls of smooth
// commands included), and arcs within their endpoints inflated by radius and
// chord bounds.
func controlBBox(cs []path.Cmd) (bbox, bool) {
	bb := bbox{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	var cx, cy, sx, sy float64
	var c2x, c2y, qcx, qcy float64
	prevC, prevQ := false, false
	add := func(x, y float64) {
		bb.minX = min(bb.minX, x)
		bb.minY = min(bb.minY, y)
		bb.maxX = max(bb.maxX, x)
		bb.maxY = max(bb.maxY, y)
	}
	for _, c := range cs {
		rel := c.Op >= 'a'
		ax := func(v float64) float64 {
			if rel {
				return cx + v
			}
			return v
		}
		ay := func(v float64) float64 {
			if rel {
				return cy + v
			}
			return v
		}
		wasC, wasQ := false, false
		switch c.Op | 0x20 {
		case 'm':
			cx, cy = ax(c.Args[0]), ay(c.Args[1])
			sx, sy = cx, cy
			add(cx, cy)
		case 'z':
			cx, cy = sx, sy
		case 'l':
			cx, cy = ax(c.Args[0]), ay(c.Args[1])
			add(cx, cy)
		case 'h':
			cx = ax(c.Args[0])
			add(cx, cy)
		case 'v':
			if rel {
				cy += c.Args[0]
			} else {
				cy = c.Args[0]
			}
			add(cx, cy)
		case 'c':
			add(ax(c.Args[0]), ay(c.Args[1]))
			c2x, c2y = ax(c.Args[2]), ay(c.Args[3])
			add(c2x, c2y)
			cx, cy = ax(c.Args[4]), ay(c.Args[5])
			add(cx, cy)
			wasC = true
		case 's':
			if prevC {
				add(2*cx-c2x, 2*cy-c2y)
			}
			c2x, c2y = ax(c.Args[0]), ay(c.Args[1])
			add(c2x, c2y)
			cx, cy = ax(c.Args[2]), ay(c.Args[3])
			add(cx, cy)
			wasC = true
		case 'q':
			qcx, qcy = ax(c.Args[0]), ay(c.Args[1])
			add(qcx, qcy)
			cx, cy = ax(c.Args[2]), ay(c.Args[3])
			add(cx, cy)
			wasQ = true
		case 't':
			if prevQ {
				qcx, qcy = 2*cx-qcx, 2*cy-qcy
			} else {
				qcx, qcy = cx, cy
			}
			add(qcx, qcy)
			cx, cy = ax(c.Args[0]), ay(c.Args[1])
			add(cx, cy)
			wasQ = true
		case 'a':
			r := 2*max(math.Abs(c.Args[0]), math.Abs(c.Args[1])) +
				math.Hypot(ax(c.Args[5])-cx, ay(c.Args[6])-cy)
			add(cx-r, cy-r)
			add(cx+r, cy+r)
			cx, cy = ax(c.Args[5]), ay(c.Args[6])
			add(cx+r, cy+r)
			add(cx-r, cy-r)
		}
		prevC, prevQ = wasC, wasQ
	}
	if math.IsInf(bb.minX, 1) {
		return bb, false
	}
	return bb, true
}
