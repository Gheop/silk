package path

import (
	"bytes"
	"math"
)

// Options controls path-data optimization.
type Options struct {
	// Precision is the number of decimals kept; negative keeps exact values.
	Precision int

	// RemoveNoops enables dropping zero-length segments and empty subpaths.
	// Only safe when the path cannot be stroked or carry markers; the caller
	// decides from document context.
	RemoveNoops bool
}

// Optimize re-encodes a command list in its shortest safe form, or returns
// nil when no stable encoding was found (the caller keeps the original).
//
// Geometry is tracked in two spaces: exact (what the input meant) and
// emitted (what a consumer of the output computes). Every emitted delta is
// taken against the emitted point, so rounding error stays below half a unit
// of the last kept decimal and never accumulates.
//
// Rounding makes some encoding choices threshold-sensitive: a shorthand that
// was not eligible against the exact input can become eligible against the
// rounded output. Optimizing repeatedly until the bytes reach a fixed point
// guarantees idempotence regardless of those thresholds.
func Optimize(cs []Cmd, o Options) []byte {
	out := optimizeOnce(cs, o)
	for range 4 {
		back, err := Parse(out)
		if err != nil {
			return nil
		}
		next := optimizeOnce(back, o)
		if bytes.Equal(next, out) {
			return out
		}
		out = next
	}
	// No fixed point within the bound: dropping the optimization is the only
	// idempotent-safe answer.
	return nil
}

func optimizeOnce(cs []Cmd, o Options) []byte {
	prec := o.Precision
	if prec > 15 {
		prec = -1 // beyond float64 decimal resolution: exact is both safer and shorter
	}
	st := state{o: o, prec: prec, tol: tolAt(prec)}
	for i, c := range cs {
		// Removing or re-basing the command before a smooth curve would
		// change that curve's reflected control point.
		st.nextRefl = i+1 < len(cs) && isSmooth(cs[i+1].Op)
		st.nextClose = i+1 < len(cs) && (cs[i+1].Op|0x20) == 'z'
		st.command(c)
	}
	if st.pending && !o.RemoveNoops {
		st.flushPending()
	}
	return st.e.b
}

func isSmooth(op byte) bool {
	l := op | 0x20
	return l == 's' || l == 't'
}

func tolAt(prec int) float64 {
	if prec < 0 {
		return 0
	}
	return 0.5 * math.Pow10(-prec)
}

// cand is one candidate encoding of a command, with the consumer-visible
// state it produces.
type cand struct {
	op         byte
	args       []float64
	prec       int   // formatting precision for this candidate's numbers
	exactMask  uint8 // format arg i exactly regardless of precision
	endX, endY float64
	c2x, c2y   float64 // resulting cubic second control, absolute
	qcx, qcy   float64 // resulting quadratic control, absolute
}

type state struct {
	o    Options
	prec int
	tol  float64
	e    emitter

	implicit byte

	cx, cy float64 // exact current point
	sx, sy float64 // exact subpath start

	ecx, ecy   float64 // emitted current point
	esx, esy   float64 // emitted subpath start
	ec2x, ec2y float64 // emitted second control of the previous cubic
	eqcx, eqcy float64 // emitted control of the previous quadratic
	pqcx, pqcy float64 // exact control of the previous quadratic

	prevCubic, prevQuad bool

	pending   bool // moveto buffered until its subpath proves non-empty
	px, py    float64
	open      bool // something was drawn since the last moveto/closepath
	nextRefl  bool
	nextClose bool

	scratch [6][]byte
}

func (st *state) q(v float64) float64 { return quantize(v, st.prec) }

// localPrec picks the precision for one command from its direction vectors,
// given as flat (dx, dy) pairs: segment chords, control-polygon edges. A
// short vector's direction — which stroke joins and curve tangents amplify
// into whole visible pixels — is destroyed when the rounding error rivals
// the vector length, so the error is kept below ~0.5% of each vector. A
// vector of exact zero has no direction and imposes nothing.
func localPrec(prec int, vecs ...float64) int {
	if prec < 0 {
		return prec
	}
	out := prec
	for i := 0; i < len(vecs); i += 2 {
		m := max(math.Abs(vecs[i]), math.Abs(vecs[i+1]))
		if m == 0 || m >= 1 {
			continue
		}
		needed := int(math.Ceil(-math.Log10(m))) + 2
		if needed > out {
			out = needed
		}
	}
	return min(out, 12)
}

// endpoint is where a command must land in emitted space.
type endpoint struct {
	x, y  float64
	exact bool // encode verbatim, bypassing precision
}

// endpointFor handles the last point before a closepath. The close segment's
// direction is the tiny vector from that point back to the subpath start;
// independent rounding of its two ends redirects it freely, and stroke joins
// amplify that into visibly different corners (a miter spike appearing or
// vanishing). When the closing vector is small enough for that to matter,
// the endpoint lands exactly on emittedStart - exactClosingVector, which
// reproduces the closing direction bit-for-bit.
func (st *state) endpointFor(x, y float64) endpoint {
	if !st.nextClose || st.pending {
		return endpoint{x, y, false}
	}
	gx, gy := st.sx-x, st.sy-y
	if gx == 0 && gy == 0 {
		return endpoint{st.esx, st.esy, true}
	}
	if math.Abs(gx) <= 20*st.tol && math.Abs(gy) <= 20*st.tol {
		return endpoint{st.esx - gx, st.esy - gy, true}
	}
	return endpoint{x, y, false}
}

func (st *state) command(c Cmd) {
	rel := c.Op >= 'a'
	ax := func(i int) float64 {
		if rel {
			return st.cx + c.Args[i]
		}
		return c.Args[i]
	}
	ay := func(i int) float64 {
		if rel {
			return st.cy + c.Args[i]
		}
		return c.Args[i]
	}
	switch c.Op | 0x20 {
	case 'm':
		x, y := ax(0), ay(1)
		if st.pending && !st.o.RemoveNoops {
			st.flushPending()
		}
		// With removal enabled, a still-buffered moveto is an empty subpath:
		// the new one simply replaces it.
		st.pending, st.px, st.py = true, x, y
		st.cx, st.cy, st.sx, st.sy = x, y, x, y
		st.prevCubic, st.prevQuad = false, false
		st.open = false
	case 'z':
		st.closePath()
	case 'l':
		st.lineTo(ax(0), ay(1))
	case 'h':
		if rel {
			st.lineTo(st.cx+c.Args[0], st.cy)
		} else {
			st.lineTo(c.Args[0], st.cy)
		}
	case 'v':
		if rel {
			st.lineTo(st.cx, st.cy+c.Args[0])
		} else {
			st.lineTo(st.cx, c.Args[0])
		}
	case 'c':
		st.cubicTo(ax(0), ay(1), ax(2), ay(3), ax(4), ay(5), false)
	case 's':
		st.cubicTo(0, 0, ax(0), ay(1), ax(2), ay(3), true)
	case 'q':
		st.quadTo(ax(0), ay(1), ax(2), ay(3), false)
	case 't':
		st.quadTo(0, 0, ax(0), ay(1), true)
	case 'a':
		st.arcTo(c.Args[0], c.Args[1], c.Args[2], c.Args[3], c.Args[4], ax(5), ay(6))
	}
}

func (st *state) closePath() {
	if st.pending {
		if st.o.RemoveNoops && !st.nextRefl {
			// Empty subpath: drop the close, keep the buffered moveto. The
			// current point returns to the subpath start either way.
			st.cx, st.cy = st.sx, st.sy
			return
		}
		st.flushPending()
	} else if !st.open && st.o.RemoveNoops && !st.nextRefl {
		// Duplicate close of an already-closed subpath.
		st.cx, st.cy = st.sx, st.sy
		return
	}
	st.choose([]cand{{op: 'z', endX: st.esx, endY: st.esy}})
	st.cx, st.cy = st.sx, st.sy
	st.prevCubic, st.prevQuad = false, false
	st.open = false
}

// refCur returns the point the consumer would be at before the next emitted
// command, accounting for a buffered moveto.
func (st *state) refCur() (float64, float64) {
	if st.pending {
		return st.q(st.px), st.q(st.py)
	}
	return st.ecx, st.ecy
}

// near reports whether every coordinate pair is within tolerance of (x, y).
func near(tol, x, y float64, pts ...float64) bool {
	for i := 0; i < len(pts); i += 2 {
		if math.Abs(pts[i]-x) > tol || math.Abs(pts[i+1]-y) > tol {
			return false
		}
	}
	return true
}

// dropNoop drops a command whose every consumer-visible point collapses onto
// the current point. pts are the absolute exact points of the command.
func (st *state) dropNoop(endX, endY float64, pts ...float64) bool {
	if !st.o.RemoveNoops || st.nextRefl {
		return false
	}
	rx, ry := st.refCur()
	qpts := make([]float64, 0, len(pts)+2)
	for _, p := range append(pts, endX, endY) {
		qpts = append(qpts, st.q(p))
	}
	if !near(st.tol, rx, ry, qpts...) {
		return false
	}
	st.cx, st.cy = endX, endY
	st.prevCubic, st.prevQuad = false, false
	return true
}

func (st *state) lineTo(x, y float64) {
	if st.dropNoop(x, y) {
		return
	}
	st.flushPending()
	et := st.endpointFor(x, y)
	lp := localPrec(st.prec, et.x-st.ecx, et.y-st.ecy)
	ql := func(v float64) float64 { return quantize(v, lp) }
	tl := tolAt(lp)
	var endMask uint8
	qe := ql
	if et.exact {
		endMask = 1<<0 | 1<<1
		qe = func(v float64) float64 { return v }
	}
	cs := make([]cand, 0, 6)
	// Eligibility compares quantized values: the second run sees the rounded
	// output, so deciding on exact inputs would flip choices between runs
	// and break idempotence. The tolerance is the local one: freezing the
	// off-axis coordinate must not bend a short segment's direction.
	hOK := math.Abs(ql(et.y)-st.ecy) <= tl || ql(et.y-st.ecy) == 0
	vOK := math.Abs(ql(et.x)-st.ecx) <= tl || ql(et.x-st.ecx) == 0
	if et.exact {
		hOK = et.y == st.ecy
		vOK = et.x == st.ecx
	}
	if hOK {
		cs = append(cs,
			cand{op: 'h', prec: lp, args: []float64{et.x - st.ecx}, exactMask: endMask & 1,
				endX: qe(et.x-st.ecx) + st.ecx, endY: st.ecy},
			cand{op: 'H', prec: lp, args: []float64{et.x}, exactMask: endMask & 1,
				endX: qe(et.x), endY: st.ecy})
	}
	if vOK {
		cs = append(cs,
			cand{op: 'v', prec: lp, args: []float64{et.y - st.ecy}, exactMask: endMask & 1,
				endX: st.ecx, endY: qe(et.y-st.ecy) + st.ecy},
			cand{op: 'V', prec: lp, args: []float64{et.y}, exactMask: endMask & 1,
				endX: st.ecx, endY: qe(et.y)})
	}
	cs = append(cs,
		cand{op: 'l', prec: lp, args: []float64{et.x - st.ecx, et.y - st.ecy}, exactMask: endMask,
			endX: st.ecx + qe(et.x-st.ecx), endY: st.ecy + qe(et.y-st.ecy)},
		cand{op: 'L', prec: lp, args: []float64{et.x, et.y}, exactMask: endMask,
			endX: qe(et.x), endY: qe(et.y)})
	st.choose(cs)
	st.cx, st.cy = x, y
	st.prevCubic, st.prevQuad = false, false
	st.open = true
}

// reflC returns the control point a smooth cubic would inherit here.
func (st *state) reflC() (float64, float64) {
	if st.prevCubic {
		return 2*st.ecx - st.ec2x, 2*st.ecy - st.ec2y
	}
	return st.ecx, st.ecy
}

func (st *state) cubicTo(c1x, c1y, c2x, c2y, x, y float64, isSmoothIn bool) {
	if isSmoothIn {
		// The input gave no explicit first control; the consumer derives it.
		c1x, c1y = st.reflC()
	}
	if st.dropNoop(x, y, c1x, c1y, c2x, c2y) {
		return
	}
	st.flushPending()
	et := st.endpointFor(x, y)
	lp := localPrec(st.prec,
		c1x-st.ecx, c1y-st.ecy, // start tangent
		c2x-c1x, c2y-c1y, // control-polygon edge
		et.x-c2x, et.y-c2y, // end tangent
		et.x-st.ecx, et.y-st.ecy) // chord
	ql := func(v float64) float64 { return quantize(v, lp) }
	tl := tolAt(lp)
	qe := ql
	if et.exact {
		qe = func(v float64) float64 { return v }
	}
	smoothOK := isSmoothIn
	if !smoothOK {
		rx, ry := st.reflC()
		smoothOK = math.Abs(ql(c1x)-rx) <= tl && math.Abs(ql(c1y)-ry) <= tl
	}
	cs := make([]cand, 0, 4)
	if smoothOK {
		var m uint8
		if et.exact {
			m = 1<<2 | 1<<3
		}
		cs = append(cs,
			cand{op: 's', prec: lp, exactMask: m,
				args: []float64{c2x - st.ecx, c2y - st.ecy, et.x - st.ecx, et.y - st.ecy},
				endX: st.ecx + qe(et.x-st.ecx), endY: st.ecy + qe(et.y-st.ecy),
				c2x: st.ecx + ql(c2x-st.ecx), c2y: st.ecy + ql(c2y-st.ecy)},
			cand{op: 'S', prec: lp, exactMask: m,
				args: []float64{c2x, c2y, et.x, et.y},
				endX: qe(et.x), endY: qe(et.y), c2x: ql(c2x), c2y: ql(c2y)})
	}
	if !isSmoothIn {
		var m uint8
		if et.exact {
			m = 1<<4 | 1<<5
		}
		cs = append(cs,
			cand{op: 'c', prec: lp, exactMask: m,
				args: []float64{c1x - st.ecx, c1y - st.ecy, c2x - st.ecx, c2y - st.ecy, et.x - st.ecx, et.y - st.ecy},
				endX: st.ecx + qe(et.x-st.ecx), endY: st.ecy + qe(et.y-st.ecy),
				c2x: st.ecx + ql(c2x-st.ecx), c2y: st.ecy + ql(c2y-st.ecy)},
			cand{op: 'C', prec: lp, exactMask: m,
				args: []float64{c1x, c1y, c2x, c2y, et.x, et.y},
				endX: qe(et.x), endY: qe(et.y), c2x: ql(c2x), c2y: ql(c2y)})
	}
	win := st.choose(cs)
	st.ec2x, st.ec2y = win.c2x, win.c2y
	st.cx, st.cy = x, y
	st.prevCubic, st.prevQuad = true, false
	st.open = true
}

func (st *state) quadTo(qx, qy, x, y float64, isSmoothIn bool) {
	// The control the consumer would derive for a smooth quadratic here.
	rx, ry := st.ecx, st.ecy
	if st.prevQuad {
		rx, ry = 2*st.ecx-st.eqcx, 2*st.ecy-st.eqcy
	}
	if isSmoothIn {
		qx, qy = rx, ry
	}
	if st.dropNoop(x, y, qx, qy) {
		return
	}
	st.flushPending()
	et := st.endpointFor(x, y)
	lp := localPrec(st.prec,
		qx-st.ecx, qy-st.ecy, // start tangent
		et.x-qx, et.y-qy, // end tangent
		et.x-st.ecx, et.y-st.ecy) // chord
	ql := func(v float64) float64 { return quantize(v, lp) }
	tl := tolAt(lp)
	qe := ql
	if et.exact {
		qe = func(v float64) float64 { return v }
	}
	smoothOK := isSmoothIn || (math.Abs(ql(qx)-rx) <= tl && math.Abs(ql(qy)-ry) <= tl)
	cs := make([]cand, 0, 4)
	if smoothOK {
		var m uint8
		if et.exact {
			m = 1<<0 | 1<<1
		}
		cs = append(cs,
			cand{op: 't', prec: lp, exactMask: m, args: []float64{et.x - st.ecx, et.y - st.ecy},
				endX: st.ecx + qe(et.x-st.ecx), endY: st.ecy + qe(et.y-st.ecy), qcx: rx, qcy: ry},
			cand{op: 'T', prec: lp, exactMask: m, args: []float64{et.x, et.y},
				endX: qe(et.x), endY: qe(et.y), qcx: rx, qcy: ry})
	}
	if !isSmoothIn {
		var m uint8
		if et.exact {
			m = 1<<2 | 1<<3
		}
		cs = append(cs,
			cand{op: 'q', prec: lp, exactMask: m,
				args: []float64{qx - st.ecx, qy - st.ecy, et.x - st.ecx, et.y - st.ecy},
				endX: st.ecx + qe(et.x-st.ecx), endY: st.ecy + qe(et.y-st.ecy),
				qcx: st.ecx + ql(qx-st.ecx), qcy: st.ecy + ql(qy-st.ecy)},
			cand{op: 'Q', prec: lp, exactMask: m, args: []float64{qx, qy, et.x, et.y},
				endX: qe(et.x), endY: qe(et.y), qcx: ql(qx), qcy: ql(qy)})
	}
	win := st.choose(cs)
	st.eqcx, st.eqcy = win.qcx, win.qcy
	st.pqcx, st.pqcy = qx, qy
	st.cx, st.cy = x, y
	st.prevCubic, st.prevQuad = false, true
	st.open = true
}

// arcMargin measures how far an arc is from the degenerate half-turn where
// the chord equals the diameter. There the arc's center goes as the square
// root of that distance, so rounding error in the chord or radii is hugely
// amplified; the emission precision must scale with the margin.
func arcMargin(rx, ry, rotDeg, dx, dy float64) float64 {
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		return math.Inf(1) // renders as a straight line; nothing to protect
	}
	phi := rotDeg * math.Pi / 180
	c, s := math.Cos(phi), math.Sin(phi)
	x1 := (c*dx + s*dy) / 2
	y1 := (-s*dx + c*dy) / 2
	lam := (x1/rx)*(x1/rx) + (y1/ry)*(y1/ry)
	return math.Abs(1-math.Sqrt(lam)) * min(rx, ry)
}

func (st *state) arcTo(rx, ry, rot, laf, sf, x, y float64) {
	// An arc whose emitted endpoints coincide is omitted by consumers, so
	// dropping it changes nothing they render.
	if st.dropNoop(x, y) {
		return
	}
	st.flushPending()
	et := st.endpointFor(x, y)
	lp := localPrec(st.prec, et.x-st.ecx, et.y-st.ecy, rx, ry,
		arcMargin(rx, ry, rot, et.x-st.ecx, et.y-st.ecy), 0)
	ql := func(v float64) float64 { return quantize(v, lp) }
	qe := ql
	var endMask uint8
	if et.exact {
		endMask = 1<<5 | 1<<6
		qe = func(v float64) float64 { return v }
	}
	// Radii that round to zero would turn the arc into a straight line;
	// consumers scale small radii up instead. Keep them exact.
	mask := endMask
	if ql(rx) == 0 && rx != 0 {
		mask |= 1 << 0
	}
	if ql(ry) == 0 && ry != 0 {
		mask |= 1 << 1
	}
	cs := []cand{
		{op: 'a', prec: lp, exactMask: mask,
			args: []float64{rx, ry, rot, laf, sf, et.x - st.ecx, et.y - st.ecy},
			endX: st.ecx + qe(et.x-st.ecx), endY: st.ecy + qe(et.y-st.ecy)},
		{op: 'A', prec: lp, exactMask: mask,
			args: []float64{rx, ry, rot, laf, sf, et.x, et.y},
			endX: qe(et.x), endY: qe(et.y)},
	}
	st.choose(cs)
	st.cx, st.cy = x, y
	st.prevCubic, st.prevQuad = false, false
	st.open = true
}

func (st *state) flushPending() {
	if !st.pending {
		return
	}
	st.pending = false
	x, y := st.px, st.py
	win := st.choose([]cand{
		{op: 'M', prec: st.prec, args: []float64{x, y}, endX: st.q(x), endY: st.q(y)},
		{op: 'm', prec: st.prec, args: []float64{x - st.ecx, y - st.ecy},
			endX: st.ecx + st.q(x-st.ecx), endY: st.ecy + st.q(y-st.ecy)},
	})
	st.esx, st.esy = win.endX, win.endY
}

// choose serializes every candidate in the current emitter context, appends
// the shortest (first wins ties), and advances the emitted state.
func (st *state) choose(cs []cand) *cand {
	best, bestLen := 0, int(^uint(0)>>1)
	var bestKind byte
	var bestOpen bool
	for i := range cs {
		e := emitter{b: st.scratch[i][:0], prevKind: st.e.prevKind, prevOpen: st.e.prevOpen}
		encodeCand(&e, st.implicit, &cs[i], cs[i].prec)
		st.scratch[i] = e.b
		if len(e.b) < bestLen {
			best, bestLen = i, len(e.b)
			bestKind, bestOpen = e.prevKind, e.prevOpen
		}
	}
	st.e.b = append(st.e.b, st.scratch[best]...)
	st.e.prevKind, st.e.prevOpen = bestKind, bestOpen
	w := &cs[best]
	st.implicit = nextImplicit(w.op)
	st.ecx, st.ecy = w.endX, w.endY
	return w
}

func encodeCand(e *emitter, implicit byte, c *cand, prec int) {
	if c.op != implicit || len(c.args) == 0 {
		e.letter(c.op)
	}
	arc := c.op|0x20 == 'a'
	for i, v := range c.args {
		switch {
		case arc && (i == 3 || i == 4):
			e.flag(v)
		case c.exactMask&(1<<i) != 0:
			e.number(v, -1)
		default:
			e.number(v, prec)
		}
	}
}
