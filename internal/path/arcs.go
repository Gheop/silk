package path

import "math"

// arcFit is a circle a run of cubics traces, with the accumulated sweep.
type arcFit struct {
	cx, cy, r float64
	ccw       bool // atan2 angle increases along the run (SVG sweep flag 1)
}

// arcSeg records one converted cubic: its endpoint and sweep contribution.
type arcSeg struct {
	ex, ey, c2x, c2y float64
	delta            float64
}

// convertArcs replaces runs of at least two consecutive cubics that trace a
// common circle — drawing tools export circles and round corners as cubic
// chains — with endpoint arcs: 7 arguments instead of 6 per curve replaced.
// Every sample must stay within the tube around the circle, and the run
// keeps a single rotation direction with total sweep under 2π (an arc whose
// endpoints coincide renders as nothing).
func convertArcs(cs []Cmd, tol float64, prec int, strokeSafe bool) []Cmd {
	if tol <= 0 {
		return cs
	}
	hasCubic := false
	for i := range cs {
		if op := cs[i].Op | 0x20; op == 'c' || op == 's' {
			hasCubic = true
			break
		}
	}
	if !hasCubic {
		return cs
	}
	out := make([]Cmd, 0, len(cs))
	var cx, cy, spX, spY float64
	var pc2x, pc2y float64 // absolute second control of the previous cubic
	prevCubic := false

	// cubicAbs resolves command k (a cubic) to absolute control points from
	// the current point, deriving a smooth command's first control the way
	// the consumer would.
	cubicAbs := func(k int) (c1x, c1y, c2x, c2y, x, y float64) {
		c := cs[k]
		rel := c.Op >= 'a'
		at := func(i int) (float64, float64) {
			if rel {
				return cx + c.Args[i], cy + c.Args[i+1]
			}
			return c.Args[i], c.Args[i+1]
		}
		if c.Op|0x20 == 's' {
			c1x, c1y = cx, cy
			if prevCubic {
				c1x, c1y = 2*cx-pc2x, 2*cy-pc2y
			}
			c2x, c2y = at(0)
			x, y = at(2)
			return
		}
		c1x, c1y = at(0)
		c2x, c2y = at(2)
		x, y = at(4)
		return
	}

	// copyCubic emits command k verbatim and advances the tracked state.
	copyCubic := func(k int) {
		_, _, c2x, c2y, x, y := cubicAbs(k)
		out = append(out, cs[k])
		pc2x, pc2y = c2x, c2y
		cx, cy = x, y
		prevCubic = true
	}

	for i := 0; i < len(cs); {
		op := cs[i].Op | 0x20
		if op != 'c' && op != 's' {
			c := cs[i]
			out = append(out, c)
			rel := c.Op >= 'a'
			switch op {
			case 'm':
				if rel {
					cx, cy = cx+c.Args[0], cy+c.Args[1]
				} else {
					cx, cy = c.Args[0], c.Args[1]
				}
				spX, spY = cx, cy
			case 'z':
				cx, cy = spX, spY
			case 'l', 'q', 't', 'a':
				n := len(c.Args)
				x, y := c.Args[n-2], c.Args[n-1]
				if rel {
					x, y = cx+x, cy+y
				}
				cx, cy = x, y
			case 'h':
				if rel {
					cx += c.Args[0]
				} else {
					cx = c.Args[0]
				}
			case 'v':
				if rel {
					cy += c.Args[0]
				} else {
					cy = c.Args[0]
				}
			}
			prevCubic = false
			i++
			continue
		}
		c1x, c1y, c2x, c2y, x, y := cubicAbs(i)
		fit, delta, ok := fitArcCircle(cx, cy, c1x, c1y, c2x, c2y, x, y, tol, prec, strokeSafe)
		if !ok {
			copyCubic(i)
			i++
			continue
		}
		group := []arcSeg{{x, y, c2x, c2y, delta}}
		// Extend over following cubics on the same circle, walking a
		// shadow of the tracked state.
		gx, gy := x, y
		gc2x, gc2y := c2x, c2y
		j := i + 1
		for j < len(cs) {
			if op := cs[j].Op | 0x20; op != 'c' && op != 's' {
				break
			}
			sc, ss := cx, cy
			spc, spq := pc2x, pc2y
			wasCubic := prevCubic
			cx, cy, pc2x, pc2y, prevCubic = gx, gy, gc2x, gc2y, true
			d1x, d1y, d2x, d2y, nx, ny := cubicAbs(j)
			cx, cy, pc2x, pc2y, prevCubic = sc, ss, spc, spq, wasCubic
			d, ok := onCircle(fit, gx, gy, d1x, d1y, d2x, d2y, nx, ny, tol, strokeSafe)
			if !ok || sweepOf(group)+d > 3.8*math.Pi {
				break
			}
			group = append(group, arcSeg{nx, ny, d2x, d2y, d})
			gx, gy, gc2x, gc2y = nx, ny, d2x, d2y
			j++
		}
		// The command after the run would reflect the last cubic's control.
		if j < len(cs) && (cs[j].Op|0x20) == 's' {
			group = group[:len(group)-1]
			j--
		}
		if len(group) < 2 {
			copyCubic(i)
			i++
			continue
		}
		// The renderer re-derives the centre from (radius, chord); near the
		// half turn that square root is ill-conditioned and a sub-tolerance
		// radius or chord change moves the arc by sqrt(Δ·2r) — pixels, not
		// sub-pixels. Every emitted segment must therefore reconstruct, with
		// the emitted radius and a chord perturbed by the worst endpoint
		// rounding, to within the fit's own tube.
		remit := fit.r
		if prec >= 0 {
			remit = quantize(fit.r, prec)
		}
		segStart := func(k int) (float64, float64) {
			if k == 0 {
				return cx, cy // current point before the run, not the first cubic's end
			}
			return group[k-1].ex, group[k-1].ey
		}
		stable := func(k int, segs []arcSeg) bool {
			s := sweepOf(segs)
			if s > 1.9*math.Pi {
				return false
			}
			sx, sy := segStart(k)
			last := segs[len(segs)-1]
			dx, dy := last.ex-sx, last.ey-sy
			l := math.Hypot(dx, dy)
			if l == 0 {
				return false
			}
			sag := fit.r
			if disc := fit.r*fit.r - l*l/4; disc > 0 {
				sag = l * l / 4 / (fit.r + math.Sqrt(disc))
			}
			tube := arcTube(fit.r, sag, tol, strokeSafe)
			eps := 0.0
			if prec >= 0 {
				eps = 2 * math.Sqrt2 * tolAt(prec)
			}
			for _, e := range [...]float64{0, eps, -eps} {
				px, py := last.ex+e*dx/l, last.ey+e*dy/l
				rcx, rcy, rr, ok := arcRenderCenter(remit, sx, sy, px, py, s > math.Pi, fit.ccw)
				if !ok || math.Hypot(rcx-fit.cx, rcy-fit.cy)+math.Abs(rr-fit.r) > tube {
					return false
				}
				if eps == 0 {
					break
				}
			}
			return true
		}
		emit := func(k int, segs []arcSeg) {
			s := sweepOf(segs)
			last := segs[len(segs)-1]
			laf, sf := 0.0, 0.0
			if s > math.Pi {
				laf = 1
			}
			if fit.ccw {
				sf = 1
			}
			out = append(out, Cmd{Op: 'A', Args: []float64{remit, remit, 0, laf, sf, last.ex, last.ey}})
		}
		// Prefer the coarsest stable splitting: the whole run when it can
		// (an endpoint arc cannot span 2π — coincident endpoints render as
		// nothing), otherwise greedy segments each validated for stability.
		var spans [][2]int
		if total := sweepOf(group); total <= 1.9*math.Pi && stable(0, group) {
			spans = [][2]int{{0, len(group)}}
		} else {
			ok := true
			for f := 0; f < len(group); {
				t := f + 1
				for t < len(group) && stable(f, group[f:t+1]) {
					t++
				}
				if t == f+1 && !stable(f, group[f:t]) {
					ok = false
					break
				}
				spans = append(spans, [2]int{f, t})
				f = t
			}
			if !ok {
				copyCubic(i)
				i++
				continue
			}
		}
		for _, sp := range spans {
			emit(sp[0], group[sp[0]:sp[1]])
		}
		last := group[len(group)-1]
		cx, cy = last.ex, last.ey
		prevCubic = false
		i += len(group)
	}
	return out
}

// arcRenderCenter mirrors the renderer's endpoint-arc centre reconstruction
// (SVG spec F.6.5, circular case): radii too small for the chord scale up
// uniformly, and the flag pair picks the side of the chord.
func arcRenderCenter(r, p0x, p0y, pex, pey float64, laf, sf bool) (float64, float64, float64, bool) {
	dx, dy := pex-p0x, pey-p0y
	l2 := dx*dx + dy*dy
	if l2 == 0 || r <= 0 {
		return 0, 0, 0, false
	}
	l := math.Sqrt(l2)
	if 2*r < l {
		r = l / 2
	}
	h := 0.0
	if disc := r*r - l2/4; disc > 0 {
		h = math.Sqrt(disc)
	}
	s := 1.0
	if laf == sf {
		s = -1
	}
	return (p0x+pex)/2 - s*h*dy/l, (p0y+pey)/2 + s*h*dx/l, r, true
}

// arcTube is the deviation budget for swapping cubics and endpoint arcs.
// Only provably unstroked, markerless geometry (the RemoveNoops
// precondition) may use the radius-relative budget that admits the kappa
// approximation: on a stroked outline the swap moves both stroke edges,
// which hairlines turn into flipped pixels.
func arcTube(r, sag, tol float64, strokeSafe bool) float64 {
	if !strokeSafe {
		return tol
	}
	return max(tol, min(5e-4*r, 0.01*sag, 0.5))
}

func sweepOf(group []arcSeg) float64 {
	t := 0.0
	for i := range group {
		t += group[i].delta
	}
	return t
}

func bezierAt(t, p0x, p0y, c1x, c1y, c2x, c2y, p3x, p3y float64) (float64, float64) {
	u := 1 - t
	a, b, c, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
	return a*p0x + b*c1x + c*c2x + d*p3x, a*p0y + b*c1y + c*c2y + d*p3y
}

// fitArcCircle fits the circle through a cubic's endpoints and midpoint and
// accepts it when the quarter samples stay inside the tube, the bulge is
// deep enough to be genuinely curved (a near-flat fit puts the center far
// away and unstably), and the sweep direction is consistent.
func fitArcCircle(p0x, p0y, c1x, c1y, c2x, c2y, p3x, p3y, tol float64, prec int, strokeSafe bool) (arcFit, float64, bool) {
	mx, my := bezierAt(0.5, p0x, p0y, c1x, c1y, c2x, c2y, p3x, p3y)
	d := 2 * (p0x*(my-p3y) + mx*(p3y-p0y) + p3x*(p0y-my))
	if math.Abs(d) < 1e-12 {
		return arcFit{}, 0, false
	}
	s0 := p0x*p0x + p0y*p0y
	sm := mx*mx + my*my
	s3 := p3x*p3x + p3y*p3y
	ux := (s0*(my-p3y) + sm*(p3y-p0y) + s3*(p0y-my)) / d
	uy := (s0*(p3x-mx) + sm*(p0x-p3x) + s3*(mx-p0x)) / d
	r := math.Hypot(p0x-ux, p0y-uy)
	if r > 1e8 || r == 0 {
		return arcFit{}, 0, false
	}
	chord2 := (p3x-p0x)*(p3x-p0x) + (p3y-p0y)*(p3y-p0y)
	if chord2 == 0 {
		return arcFit{}, 0, false
	}
	// Sagitta: bail out on near-flat curves.
	disc := r*r - chord2/4
	if disc > 0 && chord2/4/(r+math.Sqrt(disc)) < 4*tol {
		return arcFit{}, 0, false
	}
	fit := arcFit{cx: ux, cy: uy, r: r}
	// Direction from start toward the midpoint sample.
	a0 := math.Atan2(p0y-uy, p0x-ux)
	am := math.Atan2(my-uy, mx-ux)
	a1 := math.Atan2(p3y-uy, p3x-ux)
	dm := math.Mod(am-a0+4*math.Pi, 2*math.Pi)
	d1 := math.Mod(a1-a0+4*math.Pi, 2*math.Pi)
	fit.ccw = dm < d1
	delta, ok := onCircle(fit, p0x, p0y, c1x, c1y, c2x, c2y, p3x, p3y, tol, strokeSafe)
	if !ok {
		return arcFit{}, 0, false
	}
	if snapped, ok := snapRadius(fit, prec, p0x, p0y, p3x, p3y); ok {
		if d, ok := onCircle(snapped, p0x, p0y, c1x, c1y, c2x, c2y, p3x, p3y, tol, strokeSafe); ok {
			return snapped, d, true
		}
	}
	return fit, delta, true
}

// snapRadius re-fits the circle with the radius quantized at the output
// precision, recentered on the chord's perpendicular bisector nearest the
// exact fit. The fitted radius of an approximated circle carries the
// approximation noise (50.000304 for a kappa circle of 50); the drawing
// almost always means the round number.
func snapRadius(fit arcFit, prec int, p0x, p0y, p3x, p3y float64) (arcFit, bool) {
	r := quantize(fit.r, prec)
	if r == fit.r || r <= 0 {
		return arcFit{}, false
	}
	mx, my := (p0x+p3x)/2, (p0y+p3y)/2
	dx, dy := p3x-p0x, p3y-p0y
	chord2 := dx*dx + dy*dy
	disc := r*r - chord2/4
	if disc < 0 {
		return arcFit{}, false
	}
	h := math.Sqrt(disc)
	l := math.Sqrt(chord2)
	if l == 0 {
		return arcFit{}, false
	}
	// Unit normal to the chord; the exact fit picks the side.
	nx, ny := -dy/l, dx/l
	c1x, c1y := mx+h*nx, my+h*ny
	c2x, c2y := mx-h*nx, my-h*ny
	cx, cy := c1x, c1y
	if math.Hypot(c2x-fit.cx, c2y-fit.cy) < math.Hypot(c1x-fit.cx, c1y-fit.cy) {
		cx, cy = c2x, c2y
	}
	return arcFit{cx: cx, cy: cy, r: r, ccw: fit.ccw}, true
}

// onCircle verifies one cubic against an already-fitted circle and returns
// its sweep contribution: every sample within the tube, endpoint included,
// and the midpoint angularly between the endpoints (no doubling back). The
// tube widens with the radius — the standard kappa approximation tools
// export deviates from the true circle by 2.7e-4 of it, and swapping one
// for the other moves the belly by that same sub-antialiasing amount — but
// stays within 1% of this segment's own bulge: a shallow sweep on a huge
// circle is close to a straight stroke, where half a unit of drift is a
// visibly displaced line. The original endpoints are kept exactly.
func onCircle(fit arcFit, p0x, p0y, c1x, c1y, c2x, c2y, p3x, p3y, tol float64, strokeSafe bool) (float64, bool) {
	chord2 := (p3x-p0x)*(p3x-p0x) + (p3y-p0y)*(p3y-p0y)
	sag := fit.r
	if disc := fit.r*fit.r - chord2/4; disc > 0 {
		sag = chord2 / 4 / (fit.r + math.Sqrt(disc))
	}
	tube := arcTube(fit.r, sag, tol, strokeSafe)
	for _, t := range [...]float64{0.25, 0.5, 0.75, 1} {
		bx, by := bezierAt(t, p0x, p0y, c1x, c1y, c2x, c2y, p3x, p3y)
		if math.Abs(math.Hypot(bx-fit.cx, by-fit.cy)-fit.r) > tube {
			return 0, false
		}
	}
	a0 := math.Atan2(p0y-fit.cy, p0x-fit.cx)
	mx, my := bezierAt(0.5, p0x, p0y, c1x, c1y, c2x, c2y, p3x, p3y)
	am := math.Atan2(my-fit.cy, mx-fit.cx)
	a1 := math.Atan2(p3y-fit.cy, p3x-fit.cx)
	dm := math.Mod(am-a0+4*math.Pi, 2*math.Pi)
	d1 := math.Mod(a1-a0+4*math.Pi, 2*math.Pi)
	if d1 < 1e-9 || dm < 1e-9 || dm == d1 {
		return 0, false
	}
	if dm < d1 {
		if !fit.ccw {
			return 0, false
		}
		return d1, true
	}
	if fit.ccw {
		return 0, false
	}
	return 2*math.Pi - d1, true
}
