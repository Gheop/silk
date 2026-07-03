package path

import (
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

	// MergeCollinear enables folding runs of collinear line segments into
	// one. The fill and stroke are identical (same trace, same length for
	// dashes); only markers attach to the removed vertices, so the caller
	// enables this when the path cannot carry markers.
	MergeCollinear bool
}

// Optimize re-encodes a command list in its shortest safe form.
//
// Geometry is tracked in two spaces: exact (what the input meant) and
// emitted (what a consumer of the output computes). Every emitted delta is
// taken against the emitted point, so rounding error stays below half a unit
// of the last kept decimal and never accumulates.
//
// Rounding makes some encoding choices threshold-sensitive: a shorthand that
// was not eligible against the exact input can become eligible against the
// rounded output, so one run is not always a byte fixed point. The document
// pipeline reruns itself to a global fixed point, which absorbs that.
func Optimize(cs []Cmd, o Options) []byte {
	out, _ := run(cs, o, false)
	return out
}

// OptimizeEmitted additionally returns the command list the output denotes
// (exactly what Parse(output) would yield), sparing callers that iterate to
// a fixed point a re-parse per round.
func OptimizeEmitted(cs []Cmd, o Options) ([]byte, []Cmd) {
	return run(cs, o, true)
}

func run(cs []Cmd, o Options, collect bool) ([]byte, []Cmd) {
	prec := o.Precision
	if prec > 15 {
		prec = -1 // beyond float64 decimal resolution: exact is both safer and shorter
	}
	if o.MergeCollinear {
		// Straightening a chain moves its whole edge coherently — more
		// visible than pointwise rounding — so the tube is half the rounding
		// tolerance.
		cs = mergeCollinear(cs, tolAt(prec)/2)
	}
	st := state{o: o, prec: prec, tol: tolAt(prec), collect: collect}
	// Pre-size the growth points: ~10 bytes per emitted command, one emitted
	// entry per input command. Halves total allocation churn on big paths.
	st.e.b = make([]byte, 0, len(cs)*10)
	if collect {
		st.emitted = make([]Cmd, 0, len(cs))
		st.arenaBlock = min(4096, len(cs)*3+8)
	}
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
	return st.e.b, st.emitted
}

func isSmooth(op byte) bool {
	l := op | 0x20
	return l == 's' || l == 't'
}

func tolAt(prec int) float64 {
	if prec < 0 {
		return 0
	}
	if prec > 15 {
		prec = 15
	}
	return 0.5 / pow10[prec]
}

// cand is one candidate encoding of a command, with the consumer-visible
// state it produces.
type cand struct {
	op         byte
	nargs      int8
	args       [7]float64
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

	collect    bool // record the emitted command list
	emitted    []Cmd
	argArena   []float64 // backing storage for emitted Args, allocated in blocks
	arenaBlock int       // next arena block size, input-proportional at first

	candBuf [6]cand // reused candidate storage: one live set at a time
	scratch [6][]byte
}

func (st *state) arenaArgs(n int) []float64 {
	if len(st.argArena) < n {
		st.argArena = make([]float64, max(st.arenaBlock, n))
		st.arenaBlock = 4096
	}
	out := st.argArena[:n:n]
	st.argArena = st.argArena[n:]
	return out
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
		needed := 3 // ceil(-log10(m)) + 2 for m in [0.1, 1)
		for threshold := 0.1; m < threshold && needed < 12; threshold /= 10 {
			needed++
		}
		if needed > out {
			out = needed
		}
	}
	return min(out, 12)
}

// withinChordTube reports whether p lies within tolerance of segment [a, b],
// with its projection inside the segment. A curve whose control points
// satisfy this lies in the same tube (convex-hull property), so replacing it
// with the segment stays within the rounding tolerance. The tube is also
// capped at 1% of the chord length: a tiny curve can be genuinely curved
// well inside the absolute tolerance.
func withinChordTube(px, py, ax, ay, bx, by, tol float64) bool {
	dx, dy := bx-ax, by-ay
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return px == ax && py == ay
	}
	t := ((px-ax)*dx + (py-ay)*dy) / l2
	if t < 0 || t > 1 {
		return false
	}
	if rel := 0.01 * math.Sqrt(l2); rel < tol {
		tol = rel
	}
	ex, ey := ax+t*dx-px, ay+t*dy-py
	return ex*ex+ey*ey <= tol*tol
}

// mergeCollinear folds runs of line segments whose intermediate vertices all
// lie inside the tolerance tube of the run's chord, walking forward
// monotonically. Geometry, stroke trace, and dash lengths are preserved;
// only the intermediate vertices disappear.
func mergeCollinear(cs []Cmd, tol float64) []Cmd {
	out := make([]Cmd, 0, len(cs))
	var cx, cy float64
	var runX, runY float64 // anchor of the current line run
	var spX, spY float64   // subpath start, for closepath
	var mids [][2]float64  // intermediate vertices of the run
	inRun := false

	flush := func(endX, endY float64) {
		out = append(out, Cmd{Op: 'L', Args: []float64{endX, endY}})
	}
	endRun := func() {
		if inRun {
			flush(cx, cy)
			inRun = false
			mids = mids[:0]
		}
	}

	for _, c := range cs {
		rel := c.Op >= 'a'
		var nx, ny float64
		isLine := false
		switch c.Op | 0x20 {
		case 'l':
			isLine = true
			nx, ny = c.Args[0], c.Args[1]
			if rel {
				nx, ny = cx+nx, cy+ny
			}
		case 'h':
			isLine = true
			nx, ny = c.Args[0], cy
			if rel {
				nx = cx + c.Args[0]
			}
		case 'v':
			isLine = true
			nx, ny = cx, c.Args[0]
			if rel {
				ny = cy + c.Args[0]
			}
		}
		if !isLine {
			endRun()
			out = append(out, c)
			// Track the current point across non-line commands.
			switch c.Op | 0x20 {
			case 'm':
				if rel {
					cx, cy = cx+c.Args[0], cy+c.Args[1]
				} else {
					cx, cy = c.Args[0], c.Args[1]
				}
				spX, spY = cx, cy
			case 'z':
				cx, cy = spX, spY
			case 'c', 's', 'q', 't', 'a':
				n := len(c.Args)
				x, y := c.Args[n-2], c.Args[n-1]
				if rel {
					x, y = cx+x, cy+y
				}
				cx, cy = x, y
			}
			continue
		}
		if inRun && extendsRun(runX, runY, mids, cx, cy, nx, ny, tol) {
			mids = append(mids, [2]float64{cx, cy})
			cx, cy = nx, ny
			continue
		}
		endRun()
		runX, runY = cx, cy
		inRun = true
		cx, cy = nx, ny
	}
	endRun()
	return out
}

// extendsRun checks that every intermediate vertex (mids plus the current
// endpoint) sits inside the tube of [start, candidate] with monotonically
// increasing projection: no doubling back, no pivoted tube escaping.
func extendsRun(sx, sy float64, mids [][2]float64, cx, cy, nx, ny, tol float64) bool {
	dx, dy := nx-sx, ny-sy
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return false
	}
	prevT := 0.0
	check := func(px, py float64) bool {
		if !withinChordTube(px, py, sx, sy, nx, ny, tol) {
			return false
		}
		t := ((px-sx)*dx + (py-sy)*dy) / l2
		if t < prevT {
			return false
		}
		prevT = t
		return true
	}
	for _, m := range mids {
		if !check(m[0], m[1]) {
			return false
		}
	}
	return check(cx, cy)
}

// endpoint is where a command must land in emitted space.
type endpoint struct {
	x, y    float64
	exact   bool // encode verbatim, bypassing precision
	minPrec int  // lower bound on the command's emission precision
}

// withMin raises a command precision to the endpoint's floor. Exact mode
// (negative) already keeps everything and stays as is.
func (et endpoint) withMin(lp int) int {
	if lp < 0 {
		return lp
	}
	return max(lp, et.minPrec)
}

// endpointFor handles the last point before a closepath. The close segment's
// direction is the tiny vector from that point back to the subpath start;
// independent rounding of its two ends redirects it freely, and stroke joins
// amplify that into visibly different corners (a miter spike appearing or
// vanishing). When the closing vector is small enough for that to matter,
// the endpoint aims at emittedStart - exactClosingVector and the command is
// forced to the precision at which rounding error stays below ~0.5% of the
// closing vector — exact emission would spend tens of bytes per number on
// the same direction guarantee.
func (st *state) endpointFor(x, y float64) endpoint {
	if !st.nextClose || st.pending {
		return endpoint{x: x, y: y}
	}
	gx, gy := st.sx-x, st.sy-y
	if gx == 0 && gy == 0 {
		return endpoint{x: st.esx, y: st.esy, exact: true}
	}
	if math.Abs(gx) <= 20*st.tol && math.Abs(gy) <= 20*st.tol {
		return endpoint{x: st.esx - gx, y: st.esy - gy, minPrec: localPrec(st.prec, gx, gy)}
	}
	return endpoint{x: x, y: y}
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
	st.choose(append(st.candBuf[:0], cand{op: 'z', endX: st.esx, endY: st.esy}))
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
	for i := 0; i+1 < len(pts); i += 2 {
		if math.Abs(st.q(pts[i])-rx) > st.tol || math.Abs(st.q(pts[i+1])-ry) > st.tol {
			return false
		}
	}
	if math.Abs(st.q(endX)-rx) > st.tol || math.Abs(st.q(endY)-ry) > st.tol {
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
	lp := et.withMin(localPrec(st.prec, et.x-st.ecx, et.y-st.ecy))
	ql := func(v float64) float64 { return quantize(v, lp) }
	tl := tolAt(lp)
	var endMask uint8
	qe := ql
	if et.exact {
		endMask = 1<<0 | 1<<1
		qe = func(v float64) float64 { return v }
	}
	cs := st.candBuf[:0]
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
			cand{op: 'h', prec: lp, nargs: 1, args: [7]float64{et.x - st.ecx}, exactMask: endMask & 1,
				endX: qe(et.x-st.ecx) + st.ecx, endY: st.ecy},
			cand{op: 'H', prec: lp, nargs: 1, args: [7]float64{et.x}, exactMask: endMask & 1,
				endX: qe(et.x), endY: st.ecy})
	}
	if vOK {
		cs = append(cs,
			cand{op: 'v', prec: lp, nargs: 1, args: [7]float64{et.y - st.ecy}, exactMask: endMask & 1,
				endX: st.ecx, endY: qe(et.y-st.ecy) + st.ecy},
			cand{op: 'V', prec: lp, nargs: 1, args: [7]float64{et.y}, exactMask: endMask & 1,
				endX: st.ecx, endY: qe(et.y)})
	}
	cs = append(cs,
		cand{op: 'l', prec: lp, nargs: 2, args: [7]float64{et.x - st.ecx, et.y - st.ecy}, exactMask: endMask,
			endX: st.ecx + qe(et.x-st.ecx), endY: st.ecy + qe(et.y-st.ecy)},
		cand{op: 'L', prec: lp, nargs: 2, args: [7]float64{et.x, et.y}, exactMask: endMask,
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
	// A curve whose control points sit inside the tolerance tube of its
	// chord is the segment, to within the same error budget as rounding —
	// unless a smooth follower needs the control point that would vanish.
	if !st.nextRefl &&
		withinChordTube(c1x, c1y, st.cx, st.cy, x, y, st.tol) &&
		withinChordTube(c2x, c2y, st.cx, st.cy, x, y, st.tol) {
		st.lineTo(x, y)
		return
	}
	st.flushPending()
	et := st.endpointFor(x, y)
	lp := et.withMin(localPrec(st.prec,
		c1x-st.ecx, c1y-st.ecy, // start tangent
		c2x-c1x, c2y-c1y, // control-polygon edge
		et.x-c2x, et.y-c2y, // end tangent
		et.x-st.ecx, et.y-st.ecy)) // chord
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
	cs := st.candBuf[:0]
	if smoothOK {
		var m uint8
		if et.exact {
			m = 1<<2 | 1<<3
		}
		cs = append(cs,
			cand{op: 's', prec: lp, exactMask: m,
				nargs: 4, args: [7]float64{c2x - st.ecx, c2y - st.ecy, et.x - st.ecx, et.y - st.ecy},
				endX: st.ecx + qe(et.x-st.ecx), endY: st.ecy + qe(et.y-st.ecy),
				c2x: st.ecx + ql(c2x-st.ecx), c2y: st.ecy + ql(c2y-st.ecy)},
			cand{op: 'S', prec: lp, exactMask: m,
				nargs: 4, args: [7]float64{c2x, c2y, et.x, et.y},
				endX: qe(et.x), endY: qe(et.y), c2x: ql(c2x), c2y: ql(c2y)})
	}
	if !isSmoothIn {
		var m uint8
		if et.exact {
			m = 1<<4 | 1<<5
		}
		cs = append(cs,
			cand{op: 'c', prec: lp, exactMask: m,
				nargs: 6, args: [7]float64{c1x - st.ecx, c1y - st.ecy, c2x - st.ecx, c2y - st.ecy, et.x - st.ecx, et.y - st.ecy},
				endX: st.ecx + qe(et.x-st.ecx), endY: st.ecy + qe(et.y-st.ecy),
				c2x: st.ecx + ql(c2x-st.ecx), c2y: st.ecy + ql(c2y-st.ecy)},
			cand{op: 'C', prec: lp, exactMask: m,
				nargs: 6, args: [7]float64{c1x, c1y, c2x, c2y, et.x, et.y},
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
	if !st.nextRefl && withinChordTube(qx, qy, st.cx, st.cy, x, y, st.tol) {
		st.lineTo(x, y)
		return
	}
	st.flushPending()
	et := st.endpointFor(x, y)
	lp := et.withMin(localPrec(st.prec,
		qx-st.ecx, qy-st.ecy, // start tangent
		et.x-qx, et.y-qy, // end tangent
		et.x-st.ecx, et.y-st.ecy)) // chord
	ql := func(v float64) float64 { return quantize(v, lp) }
	tl := tolAt(lp)
	qe := ql
	if et.exact {
		qe = func(v float64) float64 { return v }
	}
	smoothOK := isSmoothIn || (math.Abs(ql(qx)-rx) <= tl && math.Abs(ql(qy)-ry) <= tl)
	cs := st.candBuf[:0]
	if smoothOK {
		var m uint8
		if et.exact {
			m = 1<<0 | 1<<1
		}
		cs = append(cs,
			cand{op: 't', prec: lp, exactMask: m, nargs: 2, args: [7]float64{et.x - st.ecx, et.y - st.ecy},
				endX: st.ecx + qe(et.x-st.ecx), endY: st.ecy + qe(et.y-st.ecy), qcx: rx, qcy: ry},
			cand{op: 'T', prec: lp, exactMask: m, nargs: 2, args: [7]float64{et.x, et.y},
				endX: qe(et.x), endY: qe(et.y), qcx: rx, qcy: ry})
	}
	if !isSmoothIn {
		var m uint8
		if et.exact {
			m = 1<<2 | 1<<3
		}
		cs = append(cs,
			cand{op: 'q', prec: lp, exactMask: m,
				nargs: 4, args: [7]float64{qx - st.ecx, qy - st.ecy, et.x - st.ecx, et.y - st.ecy},
				endX: st.ecx + qe(et.x-st.ecx), endY: st.ecy + qe(et.y-st.ecy),
				qcx: st.ecx + ql(qx-st.ecx), qcy: st.ecy + ql(qy-st.ecy)},
			cand{op: 'Q', prec: lp, exactMask: m, nargs: 4, args: [7]float64{qx, qy, et.x, et.y},
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
	lp := et.withMin(localPrec(st.prec, et.x-st.ecx, et.y-st.ecy, rx, ry,
		arcMargin(rx, ry, rot, et.x-st.ecx, et.y-st.ecy), 0))
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
			nargs: 7, args: [7]float64{rx, ry, rot, laf, sf, et.x - st.ecx, et.y - st.ecy},
			endX: st.ecx + qe(et.x-st.ecx), endY: st.ecy + qe(et.y-st.ecy)},
		{op: 'A', prec: lp, exactMask: mask,
			nargs: 7, args: [7]float64{rx, ry, rot, laf, sf, et.x, et.y},
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
		{op: 'M', prec: st.prec, nargs: 2, args: [7]float64{x, y}, endX: st.q(x), endY: st.q(y)},
		{op: 'm', prec: st.prec, nargs: 2, args: [7]float64{x - st.ecx, y - st.ecy},
			endX: st.ecx + st.q(x-st.ecx), endY: st.ecy + st.q(y-st.ecy)},
	})
	st.esx, st.esy = win.endX, win.endY
}

// lowerBoundLen is a cheap bound that no encoding of args can beat: at least
// the integer digits of every argument, ignoring signs, fractions, and
// separators.
func lowerBoundLen(args []float64) int {
	n := 0
	for _, v := range args {
		v = math.Abs(v)
		switch {
		case v < 10:
			n++
		case v < 100:
			n += 2
		case v < 1000:
			n += 3
		case v < 10000:
			n += 4
		case v < 100000:
			n += 5
		default:
			n += 6
		}
	}
	return n
}

// candLen computes the exact byte length a candidate will occupy in the
// current emitter context, plus the emitter state it leaves behind, without
// formatting anything. ok is false when any argument needs the slow
// formatting path; the caller then really encodes to measure.
func (st *state) candLen(c *cand) (n int, lastKind byte, lastOpen bool, ok bool) {
	kind, open := st.e.prevKind, st.e.prevOpen
	if c.op != st.implicit || c.nargs == 0 {
		n++
		kind, open = 'l', false
	}
	arc := c.op|0x20 == 'a'
	for i := range int(c.nargs) {
		if arc && (i == 3 || i == 4) {
			if kind == 'n' {
				n++ // a digit right after a number would be absorbed by it
			}
			n++
			kind = 'f'
			continue
		}
		if c.exactMask&(1<<i) != 0 {
			return 0, 0, false, false
		}
		s, fast := numInfo(c.args[i], c.prec)
		if !fast {
			return 0, 0, false, false
		}
		if kind == 'n' && !s.headMinus && !(s.headDot && open) {
			n++
		}
		n += s.length
		kind, open = 'n', s.hasDot
	}
	return n, kind, open, true
}

// choose measures every candidate in the current emitter context — by exact
// arithmetic when possible, by really encoding otherwise — then appends only
// the shortest (first wins ties) and advances the emitted state.
func (st *state) choose(cs []cand) *cand {
	best, bestLen := -1, int(^uint(0)>>1)
	var bestKind byte
	var bestOpen bool
	bestEncoded := -1 // scratch index when the best had to be really encoded
	for i := range cs {
		if lowerBoundLen(cs[i].args[:cs[i].nargs]) >= bestLen {
			continue
		}
		if n, kind, open, ok := st.candLen(&cs[i]); ok {
			if n < bestLen {
				best, bestLen = i, n
				bestKind, bestOpen = kind, open
				bestEncoded = -1
			}
			continue
		}
		e := emitter{b: st.scratch[i][:0], numBuf: st.e.numBuf, prevKind: st.e.prevKind, prevOpen: st.e.prevOpen}
		encodeCand(&e, st.implicit, &cs[i], cs[i].prec)
		st.scratch[i] = e.b
		st.e.numBuf = e.numBuf
		if len(e.b) < bestLen {
			best, bestLen = i, len(e.b)
			bestKind, bestOpen = e.prevKind, e.prevOpen
			bestEncoded = i
		}
	}
	if bestEncoded >= 0 {
		st.e.b = append(st.e.b, st.scratch[bestEncoded]...)
		st.e.prevKind, st.e.prevOpen = bestKind, bestOpen
	} else {
		mark := len(st.e.b)
		encodeCand(&st.e, st.implicit, &cs[best], cs[best].prec)
		if len(st.e.b)-mark != bestLen || st.e.prevKind != bestKind || st.e.prevOpen != bestOpen {
			// candLen and the real encoder disagree: impossible by
			// construction, but the encoder is the authority.
			bestKind, bestOpen = st.e.prevKind, st.e.prevOpen
		}
	}
	w := &cs[best]
	st.implicit = nextImplicit(w.op)
	st.ecx, st.ecy = w.endX, w.endY
	if st.collect {
		st.emitted = append(st.emitted, st.denoted(w))
	}
	return w
}

// denoted is the command a consumer parses back from this encoding: the
// values the emitted text stands for.
func (st *state) denoted(c *cand) Cmd {
	out := Cmd{Op: c.op}
	if c.nargs == 0 {
		return out
	}
	out.Args = st.arenaArgs(int(c.nargs))
	arc := c.op|0x20 == 'a'
	for i := range int(c.nargs) {
		v := c.args[i]
		if c.exactMask&(1<<i) == 0 && !(arc && (i == 3 || i == 4)) {
			v = quantize(v, c.prec)
		}
		out.Args[i] = v
	}
	return out
}

func encodeCand(e *emitter, implicit byte, c *cand, prec int) {
	if c.op != implicit || c.nargs == 0 {
		e.letter(c.op)
	}
	arc := c.op|0x20 == 'a'
	for i, v := range c.args[:c.nargs] {
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
