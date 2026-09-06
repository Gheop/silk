package path

import "math"

// Options controls path-data optimization.
type Options struct {
	// Precision is the number of decimals kept; negative keeps exact values.
	Precision int

	// RemoveNoops enables dropping zero-length segments and empty subpaths,
	// and stands the direction-preserving protections down: tiny-segment
	// precision escalation and closing-vector re-basing guard stroke joins
	// and markers, which the flag's precondition rules out. Only safe when
	// the path cannot be stroked or carry markers; the caller decides from
	// document context.
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
		// tolerance. Arc conversion removes vertices the same way straight-
		// ening does (markers would show it), hence the shared gate.
		cs = mergeCollinear(cs, tolAt(prec)/2)
		cs = convertArcs(cs, tolAt(prec)/2, prec, o.RemoveNoops)
	}
	st := state{o: o, prec: prec, tol: tolAt(prec), collect: collect}
	// Pre-size the growth points: ~10 bytes per emitted command, one emitted
	// entry per input command. Halves total allocation churn on big paths.
	st.e.b = make([]byte, 0, len(cs)*10)
	if collect {
		st.emitted = make([]Cmd, 0, len(cs))
		st.arenaBlock = min(4096, len(cs)*3+8)
	}
	inVecs := incomingVecs(cs)
	for i, c := range cs {
		// Removing or re-basing the command before a smooth curve would
		// change that curve's reflected control point.
		st.nextRefl = i+1 < len(cs) && isSmooth(cs[i+1].Op)
		st.nextClose = i+1 < len(cs) && (cs[i+1].Op|0x20) == 'z'
		st.nextVX, st.nextVY, st.nextVOK = 0, 0, false
		if i+1 < len(cs) {
			st.nextVX, st.nextVY, st.nextVOK = inVecs[i+1].x, inVecs[i+1].y, inVecs[i+1].ok
		}
		st.command(c)
	}
	if st.pending && !o.RemoveNoops {
		st.flushPending()
	}
	return st.e.b, st.emitted
}

type inVec struct {
	x, y float64
	ok   bool
}

// incomingVecs computes, for every command, the exact vector from its start
// point to its first geometric point (first control, or the endpoint for
// lines and arcs). The consumer derives that direction — a curve tangent, a
// join, an auto-oriented marker — from the EMITTED shared vertex, so it is
// corrupted by that vertex's rounding residual: the vertex must be emitted
// precisely enough for the residual to stay small against this vector.
func incomingVecs(cs []Cmd) []inVec {
	out := make([]inVec, len(cs))
	x, y, sx, sy := 0.0, 0.0, 0.0, 0.0
	for i, c := range cs {
		rel := c.Op >= 'a'
		ox, oy := 0.0, 0.0
		if rel {
			ox, oy = x, y
		}
		switch c.Op | 0x20 {
		case 'm':
			x, y = ox+c.Args[0], oy+c.Args[1]
			sx, sy = x, y
		case 'z':
			x, y = sx, sy
		case 'l':
			out[i] = inVec{ox + c.Args[0] - x, oy + c.Args[1] - y, true}
			x, y = ox+c.Args[0], oy+c.Args[1]
		case 'h':
			nx := c.Args[0]
			if rel {
				nx += x
			}
			out[i] = inVec{nx - x, 0, true}
			x = nx
		case 'v':
			ny := c.Args[0]
			if rel {
				ny += y
			}
			out[i] = inVec{0, ny - y, true}
			y = ny
		case 'c':
			out[i] = inVec{ox + c.Args[0] - x, oy + c.Args[1] - y, true}
			x, y = ox+c.Args[4], oy+c.Args[5]
		case 's':
			// The first control is a reflection; the second edge is the
			// first vertex-anchored vector the consumer computes.
			out[i] = inVec{ox + c.Args[0] - x, oy + c.Args[1] - y, true}
			x, y = ox+c.Args[2], oy+c.Args[3]
		case 'q':
			out[i] = inVec{ox + c.Args[0] - x, oy + c.Args[1] - y, true}
			x, y = ox+c.Args[2], oy+c.Args[3]
		case 't':
			out[i] = inVec{ox + c.Args[0] - x, oy + c.Args[1] - y, true}
			x, y = ox+c.Args[0], oy+c.Args[1]
		case 'a':
			out[i] = inVec{ox + c.Args[5] - x, oy + c.Args[6] - y, true}
			x, y = ox+c.Args[5], oy+c.Args[6]
		}
	}
	return out
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

	nextVX, nextVY float64 // exact incoming vector of the next command
	nextVOK        bool

	pending    bool // moveto buffered until its subpath proves non-empty
	px, py     float64
	pendingMin int  // endpoint precision the buffered moveto must honor
	open       bool // something was drawn since the last moveto/closepath
	nextRefl   bool
	nextClose  bool

	collect    bool // record the emitted command list
	emitted    []Cmd
	argArena   []float64 // backing storage for emitted Args, allocated in blocks
	arenaBlock int       // next arena block size, input-proportional at first

	candBuf [8]cand // reused candidate storage: one live set at a time
	scratch [8][]byte
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

// dirPrec is localPrec for a fillable segment: direction preservation only
// matters when a stroke join or marker can amplify it, which the RemoveNoops
// precondition rules out. Arcs keep full escalation either way — there the
// vectors bound the center's position, which is fill-visible.
func (st *state) dirPrec(vecs ...float64) int {
	if st.o.RemoveNoops {
		return st.prec
	}
	return localPrec(st.prec, vecs...)
}

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

	var arena []float64 // shared backing for emitted Args: block allocations
	arenaBlock := 8     // most paths flush a handful of runs; big ones double up
	flush := func(endX, endY float64) {
		if len(arena) < 2 {
			arena = make([]float64, arenaBlock)
			arenaBlock = min(arenaBlock*4, 4096)
		}
		args := arena[:2:2]
		arena = arena[2:]
		args[0], args[1] = endX, endY
		out = append(out, Cmd{Op: 'L', Args: args})
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
	minPrec int // lower bound on the command's emission precision
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
// lookaheadPrec is the endpoint precision the next command's first vector
// demands. It only engages for genuinely tiny vectors: beyond ~0.05 units
// the residual's direction error is under a degree, which no join, marker
// or dash renders at the tolerances the gate measures.
func (st *state) lookaheadPrec() int {
	if !st.nextVOK || st.o.RemoveNoops {
		return -1
	}
	if m := max(math.Abs(st.nextVX), math.Abs(st.nextVY)); m == 0 || m >= 0.05 {
		return -1
	}
	return localPrec(st.prec, st.nextVX, st.nextVY)
}

func (st *state) endpointFor(x, y float64) endpoint {
	// A segment whose exact endpoint is the exact current point is a stall:
	// re-basing it onto the exact target would give it the length of the
	// rounding residue and a direction stroked joins render. Pin it to the
	// emitted point; the residue does not accumulate (it stays the current
	// point's own).
	if x == st.cx && y == st.cy && !st.o.RemoveNoops {
		return endpoint{x: st.ecx, y: st.ecy}
	}
	// The next command's first direction is computed from this endpoint as
	// emitted: keep the rounding residual small against that vector, or a
	// tiny tangent (huge stroke joins, auto-oriented markers) rotates.
	nextMin := st.lookaheadPrec()
	// The closing vector only shows through the stroke's join; a fill
	// closes to the start point wherever the endpoint rounds to.
	if !st.nextClose || st.pending || st.o.RemoveNoops {
		return endpoint{x: x, y: y, minPrec: nextMin}
	}
	gx, gy := st.sx-x, st.sy-y
	if gx == 0 && gy == 0 {
		// A perfectly closed subpath: land on the emitted start to twelve
		// decimals, which is zero for every renderer (f32 internally).
		return endpoint{x: st.esx, y: st.esy, minPrec: 12}
	}
	if math.Abs(gx) <= 20*st.tol && math.Abs(gy) <= 20*st.tol {
		return endpoint{x: st.esx - gx, y: st.esy - gy, minPrec: max(nextMin, localPrec(st.prec, gx, gy))}
	}
	return endpoint{x: x, y: y, minPrec: nextMin}
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
		// The command after this moveto derives its first direction from
		// the moveto's emitted point: same lookahead as endpointFor.
		st.pendingMin = st.lookaheadPrec()
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
	// Only the exact point advances: nothing was emitted, so the smooth-
	// reflection state still describes the last command the consumer will
	// actually see. Clearing it here made a later curve eligible for s/S
	// against the wrong reflection base.
	st.cx, st.cy = endX, endY
	return true
}

func (st *state) lineTo(x, y float64) {
	if st.dropNoop(x, y) {
		return
	}
	st.flushPending()
	et := st.endpointFor(x, y)
	lp := et.withMin(st.dirPrec(et.x-st.ecx, et.y-st.ecy))
	ql := func(v float64) float64 { return quantize(v, lp) }
	tl := tolAt(lp)
	cs := st.candBuf[:0]
	// Eligibility compares quantized values: the second run sees the rounded
	// output, so deciding on exact inputs would flip choices between runs
	// and break idempotence. The tolerance is the local one: freezing the
	// off-axis coordinate must not bend a short segment's direction.
	hOK := math.Abs(ql(et.y)-st.ecy) <= tl || ql(et.y-st.ecy) == 0
	vOK := math.Abs(ql(et.x)-st.ecx) <= tl || ql(et.x-st.ecx) == 0
	if hOK {
		cs = append(cs,
			cand{op: 'h', prec: lp, nargs: 1, args: [7]float64{et.x - st.ecx},
				endX: ql(et.x-st.ecx) + st.ecx, endY: st.ecy},
			cand{op: 'H', prec: lp, nargs: 1, args: [7]float64{et.x},
				endX: ql(et.x), endY: st.ecy})
	}
	if vOK {
		cs = append(cs,
			cand{op: 'v', prec: lp, nargs: 1, args: [7]float64{et.y - st.ecy},
				endX: st.ecx, endY: ql(et.y-st.ecy) + st.ecy},
			cand{op: 'V', prec: lp, nargs: 1, args: [7]float64{et.y},
				endX: st.ecx, endY: ql(et.y)})
	}
	cs = append(cs,
		cand{op: 'l', prec: lp, nargs: 2, args: [7]float64{et.x - st.ecx, et.y - st.ecy},
			endX: st.ecx + ql(et.x-st.ecx), endY: st.ecy + ql(et.y-st.ecy)},
		cand{op: 'L', prec: lp, nargs: 2, args: [7]float64{et.x, et.y},
			endX: ql(et.x), endY: ql(et.y)})
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
	// No previous cubic: the consumer reflects onto its current point,
	// which a still-buffered moveto is about to move.
	return st.refCur()
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
	// unless a smooth follower needs the control point that would vanish,
	// or an endpoint handle is degenerate on a possibly stroked path: a
	// control on its anchor makes the endpoint tangent vanish, which
	// renderers join differently than the line's sharp angle (a miter at
	// full stroke width where the input drew none).
	degenerateHandle := (c1x == st.cx && c1y == st.cy) || (c2x == x && c2y == y)
	if !st.nextRefl && (st.o.RemoveNoops || !degenerateHandle) &&
		withinChordTube(c1x, c1y, st.cx, st.cy, x, y, st.tol) &&
		withinChordTube(c2x, c2y, st.cx, st.cy, x, y, st.tol) {
		st.lineTo(x, y)
		return
	}
	st.flushPending()
	et := st.endpointFor(x, y)
	// A control point that exactly coincides with its anchor is a degenerate
	// handle: the tangent it denotes has no direction, and the rebasing
	// residue must not invent one — a stroke cap or join renders whatever
	// direction the residue happens to point in. Pin it to the emitted
	// anchor instead.
	c1relX, c1relY, c1absX, c1absY := c1x-st.ecx, c1y-st.ecy, c1x, c1y
	if !isSmoothIn && c1x == st.cx && c1y == st.cy {
		c1relX, c1relY, c1absX, c1absY = 0, 0, st.ecx, st.ecy
	}
	c2relX, c2relY, c2absX, c2absY := c2x-st.ecx, c2y-st.ecy, c2x, c2y
	if c2x == x && c2y == y {
		c2relX, c2relY = et.x-st.ecx, et.y-st.ecy
		c2absX, c2absY = et.x, et.y
	} else if c2x == st.cx && c2y == st.cy {
		// A second control on the START anchor carries the outgoing tangent
		// of a doubly degenerate curve; keeping its absolute position while
		// the anchor rounds would leave it BEHIND the new anchor and flip
		// that tangent (a stroked miter renders the flip).
		c2relX, c2relY, c2absX, c2absY = 0, 0, st.ecx, st.ecy
	}
	lp := et.withMin(st.dirPrec(
		c1relX, c1relY, // start tangent (zero when pinned: no direction)
		c2x-c1x, c2y-c1y, // control-polygon edge
		et.x-c2x, et.y-c2y, // end tangent
		et.x-st.ecx, et.y-st.ecy)) // chord
	ql := func(v float64) float64 { return quantize(v, lp) }
	tl := tolAt(lp)
	smoothOK := isSmoothIn
	if !smoothOK {
		rx, ry := st.reflC()
		smoothOK = math.Abs(ql(c1x)-rx) <= tl && math.Abs(ql(c1y)-ry) <= tl
	}
	cs := st.candBuf[:0]
	if smoothOK {
		cs = append(cs,
			cand{op: 's', prec: lp,
				nargs: 4, args: [7]float64{c2relX, c2relY, et.x - st.ecx, et.y - st.ecy},
				endX: st.ecx + ql(et.x-st.ecx), endY: st.ecy + ql(et.y-st.ecy),
				c2x: st.ecx + ql(c2relX), c2y: st.ecy + ql(c2relY)},
			cand{op: 'S', prec: lp,
				nargs: 4, args: [7]float64{c2absX, c2absY, et.x, et.y},
				endX: ql(et.x), endY: ql(et.y), c2x: ql(c2absX), c2y: ql(c2absY)})
	}
	if !isSmoothIn {
		cs = append(cs,
			cand{op: 'c', prec: lp,
				nargs: 6, args: [7]float64{c1relX, c1relY, c2relX, c2relY, et.x - st.ecx, et.y - st.ecy},
				endX: st.ecx + ql(et.x-st.ecx), endY: st.ecy + ql(et.y-st.ecy),
				c2x: st.ecx + ql(c2relX), c2y: st.ecy + ql(c2relY)},
			cand{op: 'C', prec: lp,
				nargs: 6, args: [7]float64{c1absX, c1absY, c2absX, c2absY, et.x, et.y},
				endX: ql(et.x), endY: ql(et.y), c2x: ql(c2absX), c2y: ql(c2absY)})
	}
	// A cubic whose two quadratic pullbacks agree is an elevated quadratic:
	// the same curve within the rounding budget, two arguments fewer. A
	// smooth follower would reflect a control this rewrite discards. Only
	// clearly curved curves qualify: renderers flatten long near-straight
	// quadratics and cubics into visibly different polylines, so a control
	// hugging the chord (under 0.5% of it) keeps the original form.
	var cqx, cqy float64
	isQuad := false
	// The demotion is restricted to provably unstroked geometry, like arc
	// conversion: renderers tessellate quadratics and cubics differently,
	// and on a large thin-stroked curve that difference is a visible
	// sub-pixel ripple a fill would hide. The gate also rules out the
	// degenerate-handle stalls whose zero chord passes the bulge test
	// trivially.
	if !st.nextRefl && st.o.RemoveNoops {
		q1x, q1y := (3*c1x-st.cx)/2, (3*c1y-st.cy)/2
		q2x, q2y := (3*c2x-x)/2, (3*c2y-y)/2
		dx, dy := q1x-q2x, q1y-q2y
		chx, chy := x-st.cx, y-st.cy
		// |cross(Q-p0, chord)| = control-to-chord distance × chord length.
		bulge := math.Abs(((q1x+q2x)/2-st.cx)*chy - ((q1y+q2y)/2-st.cy)*chx)
		// Max pointwise deviation between the cubic and the elevated
		// quadratic with the averaged control is |Q1-Q2|/4.
		if dx*dx+dy*dy <= 16*st.tol*st.tol &&
			bulge >= 0.005*(chx*chx+chy*chy) {
			isQuad = true
			cqx, cqy = (q1x+q2x)/2, (q1y+q2y)/2
			rx, ry := st.ecx, st.ecy
			if st.prevQuad {
				rx, ry = 2*st.ecx-st.eqcx, 2*st.ecy-st.eqcy
			}
			if math.Abs(ql(cqx)-rx) <= tl && math.Abs(ql(cqy)-ry) <= tl {
				cs = append(cs,
					cand{op: 't', prec: lp, nargs: 2, args: [7]float64{et.x - st.ecx, et.y - st.ecy},
						endX: st.ecx + ql(et.x-st.ecx), endY: st.ecy + ql(et.y-st.ecy), qcx: rx, qcy: ry},
					cand{op: 'T', prec: lp, nargs: 2, args: [7]float64{et.x, et.y},
						endX: ql(et.x), endY: ql(et.y), qcx: rx, qcy: ry})
			}
			cs = append(cs,
				cand{op: 'q', prec: lp,
					nargs: 4, args: [7]float64{cqx - st.ecx, cqy - st.ecy, et.x - st.ecx, et.y - st.ecy},
					endX: st.ecx + ql(et.x-st.ecx), endY: st.ecy + ql(et.y-st.ecy),
					qcx: st.ecx + ql(cqx-st.ecx), qcy: st.ecy + ql(cqy-st.ecy)},
				cand{op: 'Q', prec: lp,
					nargs: 4, args: [7]float64{cqx, cqy, et.x, et.y},
					endX: ql(et.x), endY: ql(et.y), qcx: ql(cqx), qcy: ql(cqy)})
		}
	}
	win := st.choose(cs)
	st.cx, st.cy = x, y
	st.open = true
	if isQuad && (win.op|0x20 == 'q' || win.op|0x20 == 't') {
		st.eqcx, st.eqcy = win.qcx, win.qcy
		st.pqcx, st.pqcy = cqx, cqy
		st.prevCubic, st.prevQuad = false, true
		return
	}
	st.ec2x, st.ec2y = win.c2x, win.c2y
	st.prevCubic, st.prevQuad = true, false
}

func (st *state) quadTo(qx, qy, x, y float64, isSmoothIn bool) {
	// The control the consumer would derive for a smooth quadratic here.
	// With a moveto still buffered, the consumer's point is the moveto's
	// target — not the pre-moveto position, whose coincidence with the
	// control would wrongly qualify a t/T that renders as a line.
	rx, ry := st.refCur()
	if st.prevQuad {
		rx, ry = 2*st.ecx-st.eqcx, 2*st.ecy-st.eqcy
	}
	if isSmoothIn {
		qx, qy = rx, ry
	}
	if st.dropNoop(x, y, qx, qy) {
		return
	}
	// Same degenerate-handle rule as cubicTo: a control on either anchor
	// zeroes that endpoint tangent, which a stroked join renders.
	qDegenerate := (qx == st.cx && qy == st.cy) || (qx == x && qy == y)
	if !st.nextRefl && (st.o.RemoveNoops || !qDegenerate) &&
		withinChordTube(qx, qy, st.cx, st.cy, x, y, st.tol) {
		st.lineTo(x, y)
		return
	}
	st.flushPending()
	et := st.endpointFor(x, y)
	// Same degenerate-handle pin as cubicTo: a control on its anchor means
	// "no tangent", and the rebasing residue must not give it one.
	qrelX, qrelY, qabsX, qabsY := qx-st.ecx, qy-st.ecy, qx, qy
	if !isSmoothIn && qx == st.cx && qy == st.cy {
		qrelX, qrelY, qabsX, qabsY = 0, 0, st.ecx, st.ecy
	} else if qx == x && qy == y {
		qrelX, qrelY = et.x-st.ecx, et.y-st.ecy
		qabsX, qabsY = et.x, et.y
	}
	lp := et.withMin(st.dirPrec(
		qrelX, qrelY, // start tangent (zero when pinned: no direction)
		et.x-qx, et.y-qy, // end tangent
		et.x-st.ecx, et.y-st.ecy)) // chord
	ql := func(v float64) float64 { return quantize(v, lp) }
	tl := tolAt(lp)
	smoothOK := isSmoothIn || (math.Abs(ql(qx)-rx) <= tl && math.Abs(ql(qy)-ry) <= tl)
	cs := st.candBuf[:0]
	if smoothOK {
		cs = append(cs,
			cand{op: 't', prec: lp, nargs: 2, args: [7]float64{et.x - st.ecx, et.y - st.ecy},
				endX: st.ecx + ql(et.x-st.ecx), endY: st.ecy + ql(et.y-st.ecy), qcx: rx, qcy: ry},
			cand{op: 'T', prec: lp, nargs: 2, args: [7]float64{et.x, et.y},
				endX: ql(et.x), endY: ql(et.y), qcx: rx, qcy: ry})
	}
	if !isSmoothIn {
		cs = append(cs,
			cand{op: 'q', prec: lp,
				nargs: 4, args: [7]float64{qrelX, qrelY, et.x - st.ecx, et.y - st.ecy},
				endX: st.ecx + ql(et.x-st.ecx), endY: st.ecy + ql(et.y-st.ecy),
				qcx: st.ecx + ql(qrelX), qcy: st.ecy + ql(qrelY)},
			cand{op: 'Q', prec: lp, nargs: 4, args: [7]float64{qabsX, qabsY, et.x, et.y},
				endX: ql(et.x), endY: ql(et.y), qcx: ql(qabsX), qcy: ql(qabsY)})
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
	// Near the degenerate half-turn the center's distance from the chord
	// goes as the square root of the margin: any change to the chord —
	// even rounding either endpoint independently — moves the arc without
	// bound. Only reproducing the exact input chord keeps the figure, so
	// the endpoint is re-based on the emitted start and the chord is
	// emitted verbatim (quantized beyond float noise).
	// The other ill-conditioned pole is the nearly closed large arc: the
	// centre sits ~r from the chord midpoint along its normal, so an error
	// δ in a chord of length l rotates the whole figure by δ·r/l — at
	// l ≪ r, hundreds of times the rounding tolerance. The re-based chord
	// already carries the start point's residue, so no endpoint precision
	// fixes it; only the exact chord vector does (the figure then merely
	// translates by the residue instead of rotating).
	nearlyClosed := laf != 0 && 3*math.Hypot(x-st.cx, y-st.cy) < min(math.Abs(rx), math.Abs(ry))
	if cvx, cvy := x-st.cx, y-st.cy; st.tol > 0 &&
		(arcMargin(rx, ry, rot, cvx, cvy) <= 20*st.tol || nearlyClosed) {
		cvx, cvy = quantize(cvx, 12), quantize(cvy, 12)
		st.choose(append(st.candBuf[:0], cand{op: 'a', prec: 12,
			nargs: 7, args: [7]float64{rx, ry, rot, laf, sf, cvx, cvy},
			endX: st.ecx + cvx, endY: st.ecy + cvy}))
		st.cx, st.cy = x, y
		st.prevCubic, st.prevQuad = false, false
		st.open = true
		return
	}
	et := st.endpointFor(x, y)
	lp := et.withMin(localPrec(st.prec, et.x-st.ecx, et.y-st.ecy, rx, ry,
		arcMargin(rx, ry, rot, et.x-st.ecx, et.y-st.ecy), 0))
	ql := func(v float64) float64 { return quantize(v, lp) }
	// Radii that round to zero would turn the arc into a straight line;
	// consumers scale small radii up instead. Keep them exact.
	var mask uint8
	if ql(rx) == 0 && rx != 0 {
		mask |= 1 << 0
	}
	if ql(ry) == 0 && ry != 0 {
		mask |= 1 << 1
	}
	cs := []cand{
		{op: 'a', prec: lp, exactMask: mask,
			nargs: 7, args: [7]float64{rx, ry, rot, laf, sf, et.x - st.ecx, et.y - st.ecy},
			endX: st.ecx + ql(et.x-st.ecx), endY: st.ecy + ql(et.y-st.ecy)},
		{op: 'A', prec: lp, exactMask: mask,
			nargs: 7, args: [7]float64{rx, ry, rot, laf, sf, et.x, et.y},
			endX: ql(et.x), endY: ql(et.y)},
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
	lp := st.prec
	if st.prec >= 0 && st.pendingMin > lp {
		lp = min(st.pendingMin, 12)
	}
	ql := func(v float64) float64 { return quantize(v, lp) }
	win := st.choose([]cand{
		{op: 'M', prec: lp, nargs: 2, args: [7]float64{x, y}, endX: ql(x), endY: ql(y)},
		{op: 'm', prec: lp, nargs: 2, args: [7]float64{x - st.ecx, y - st.ecy},
			endX: st.ecx + ql(x-st.ecx), endY: st.ecy + ql(y-st.ecy)},
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
