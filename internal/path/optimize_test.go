package path

import (
	"math"
	"testing"
)

func opt(t *testing.T, d string, o Options) string {
	t.Helper()
	cs, err := Parse([]byte(d))
	if err != nil {
		t.Fatalf("Parse(%q): %v", d, err)
	}
	out := Optimize(cs, o)
	if _, err := Parse(out); err != nil {
		t.Fatalf("Optimize(%q) = %q does not re-parse: %v", d, out, err)
	}
	return string(out)
}

var exact = Options{Precision: -1}

func TestOptimizeEncodingChoices(t *testing.T) {
	cases := []struct {
		in   string
		o    Options
		want string
	}{
		// Relative wins on large coordinates.
		{"M100 100L110 110", exact, "M100 100l10 10"},
		// Axis-aligned linetos become h/v.
		{"M0 0L10 0", exact, "M0 0h10"},
		{"M0 0L0 10", exact, "M0 0v10"},
		{"M0 0H10V10", exact, "M0 0h10v10"},
		// Smooth cubic when the control mirrors the previous one.
		{
			"M0 0C10 10 20 10 30 0C40 -10 50 -10 60 0",
			exact,
			"M0 0c10 10 20 10 30 0s20-10 30 0",
		},
		// Smooth quadratic.
		{"M0 0Q5 5 10 0Q15 -5 20 0", exact, "M0 0q5 5 10 0t10 0"},
		// First cubic with control at the current point is smooth too.
		{"M0 0C0 0 20 10 30 0", exact, "M0 0s20 10 30 0"},
		// Arc stays an arc, flags repack.
		{"M0 0A5 5 0 0 1 10 10", exact, "M0 0a5 5 0 0110 10"},
		// Absolute wins when relative deltas are longer (and rides the
		// implicit repeat after the moveto).
		{"M100.5 100.5L0 0", exact, "M100.5 100.5 0 0"},
		// Rounding.
		{"M0 0L1.23456 7.891011", Options{Precision: 2}, "M0 0l1.23 7.89"},
		// Implicit repeats survive.
		{"M0 0L1 2L3 4L5 6", exact, "M0 0l1 2 2 2 2 2"},
		// z is preserved and lowercased.
		{"M0 0L5 5Z", exact, "M0 0l5 5z"},
	}
	for _, tc := range cases {
		if got := opt(t, tc.in, tc.o); got != tc.want {
			t.Errorf("Optimize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestOptimizeNoopRemoval(t *testing.T) {
	on := Options{Precision: -1, RemoveNoops: true}
	cases := []struct {
		in   string
		o    Options
		want string
	}{
		{"M0 0L0 0L10 10", on, "M0 0l10 10"},
		// Gate off: the zero-length segment survives (as a shorthand).
		{"M0 0L0 0L10 10", exact, "M0 0h0l10 10"},
		// Empty subpaths drop, including their close.
		{"M0 0L5 5M10 10M20 20L30 30", on, "M0 0l5 5M20 20l10 10"},
		{"M0 0L5 5M10 10z", on, "M0 0l5 5"},
		{"M0 0L5 5M10 10", on, "M0 0l5 5"},
		// Duplicate close.
		{"M0 0L5 5zz", on, "M0 0l5 5z"},
		// A zero-length segment before a smooth command must stay: removing
		// it would change the smooth command's reflected control point.
		{
			"M0 0C1 1 2 2 3 3L3 3S5 5 6 6",
			on,
			"M0 0c1 1 2 2 3 3h0s2 2 3 3",
		},
		// Zero-length curve drops when nothing reflects off it.
		{"M0 0C0 0 0 0 0 0L5 5", on, "M0 0l5 5"},
		// The subpath start moves with a replaced moveto.
		{"M1 1M2 2l1 1z", on, "M2 2l1 1z"},
	}
	for _, tc := range cases {
		if got := opt(t, tc.in, tc.o); got != tc.want {
			t.Errorf("Optimize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// In exact mode the emitted commands must replay to bit-identical geometry.
func TestOptimizeExactGeometry(t *testing.T) {
	paths := []string{
		"M100 100L110 110H5V-3l-2-2z",
		"M0 0C10 10 20 10 30 0C40 -10 50 -10 60 0S80 10 90 0",
		"M0 0Q5 5 10 0Q15 -5 20 0T30 0t5 5",
		"M0 0A5 5 30 1 0 10 10a2 2 0 015 5",
		"M.5.5l.1.1c.1.1 .2.2 .3.3",
		"M1e3 1e-3L2e3 5",
	}
	for _, p := range paths {
		cs, _ := Parse([]byte(p))
		out := Optimize(cs, exact)
		back, err := Parse(out)
		if err != nil {
			t.Errorf("%q -> %q: %v", p, out, err)
			continue
		}
		a, b := trace(cs), trace(back)
		if len(a) != len(b) {
			t.Errorf("%q -> %q: trace length %d != %d", p, out, len(a), len(b))
			continue
		}
		for i := range a {
			if a[i] != b[i] {
				t.Errorf("%q -> %q: trace[%d] = %v, want %v", p, out, i, b[i], a[i])
			}
		}
	}
}

// Rounding error must not accumulate across long runs of relative commands.
func TestOptimizeNoDrift(t *testing.T) {
	d := "M0 0"
	for range 1000 {
		d += "l.333 0"
	}
	cs, _ := Parse([]byte(d))
	out := Optimize(cs, Options{Precision: 2})
	back, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	tr := trace(back)
	endX := tr[len(tr)-1][0]
	if math.Abs(endX-333.0) > 0.005 {
		t.Errorf("end x = %v, want 333 within 0.005", endX)
	}
}

// trace replays a command list, returning every consumer-visible absolute
// point (control points included, reflections resolved).
func trace(cs []Cmd) [][2]float64 {
	var out [][2]float64
	var cx, cy, sx, sy float64
	var c2x, c2y, qcx, qcy float64
	prevC, prevQ := false, false
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
		case 'z':
			cx, cy = sx, sy
		case 'l', 't':
			if c.Op|0x20 == 't' {
				tx, ty := cx, cy
				if prevQ {
					tx, ty = 2*cx-qcx, 2*cy-qcy
				}
				qcx, qcy = tx, ty
				out = append(out, [2]float64{tx, ty})
				wasQ = true
			}
			cx, cy = ax(c.Args[0]), ay(c.Args[1])
		case 'h':
			cx = ax(c.Args[0])
		case 'v':
			if rel {
				cy += c.Args[0]
			} else {
				cy = c.Args[0]
			}
		case 'c':
			out = append(out, [2]float64{ax(c.Args[0]), ay(c.Args[1])})
			c2x, c2y = ax(c.Args[2]), ay(c.Args[3])
			out = append(out, [2]float64{c2x, c2y})
			cx, cy = ax(c.Args[4]), ay(c.Args[5])
			wasC = true
		case 's':
			tx, ty := cx, cy
			if prevC {
				tx, ty = 2*cx-c2x, 2*cy-c2y
			}
			out = append(out, [2]float64{tx, ty})
			c2x, c2y = ax(c.Args[0]), ay(c.Args[1])
			out = append(out, [2]float64{c2x, c2y})
			cx, cy = ax(c.Args[2]), ay(c.Args[3])
			wasC = true
		case 'q':
			qcx, qcy = ax(c.Args[0]), ay(c.Args[1])
			out = append(out, [2]float64{qcx, qcy})
			cx, cy = ax(c.Args[2]), ay(c.Args[3])
			wasQ = true
		case 'a':
			out = append(out, [2]float64{c.Args[0], c.Args[1]})
			out = append(out, [2]float64{c.Args[3], c.Args[4]})
			cx, cy = ax(c.Args[5]), ay(c.Args[6])
		}
		prevC, prevQ = wasC, wasQ
		out = append(out, [2]float64{cx, cy})
	}
	return out
}
