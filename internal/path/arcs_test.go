package path

import (
	"math"
	"strings"
	"testing"
)

func TestConvertArcsCircle(t *testing.T) {
	// A circle exported the usual way: four kappa cubics.
	in := "M100 50C100 77.614 77.614 100 50 100C22.386 100 0 77.614 0 50C0 22.386 22.386 0 50 0C77.614 0 100 22.386 100 50Z"
	got := opt(t, in, Options{Precision: 3, RemoveNoops: true, MergeCollinear: true})
	if !strings.Contains(strings.ToLower(got), "a") {
		t.Fatalf("no arc emitted: %q", got)
	}
	if strings.ContainsAny(got, "cCsS") {
		t.Errorf("cubics survived: %q", got)
	}
	if len(got) >= len(in) {
		t.Errorf("no byte win: %d -> %d (%q)", len(in), len(got), got)
	}
	assertOnCircle(t, got, 50, 50, 50)
}

func TestConvertArcsKeepsReflectionBase(t *testing.T) {
	// The S after the run leaves the circle, so it is not absorbed; it
	// reflects the control of the run's last cubic, which must therefore
	// stay a literal curve while the leading pair still converts.
	in := "M100 50C100 77.614 77.614 100 50 100C22.386 100 0 77.614 0 50C0 22.386 22.386 0 50 0S100 10 90 40"
	got := opt(t, in, Options{Precision: 3, RemoveNoops: true, MergeCollinear: true})
	if !strings.ContainsAny(got, "aA") {
		t.Errorf("no arc for the leading curves: %q", got)
	}
	if !strings.ContainsAny(got, "csCS") {
		t.Errorf("reflection base converted away: %q", got)
	}
}

func TestConvertArcsSmoothInsideRun(t *testing.T) {
	// A smooth cubic on the same circle is part of the run, reflection
	// and all.
	in := "M100 50C100 77.614 77.614 100 50 100S0 77.614 0 50"
	got := opt(t, in, Options{Precision: 3, RemoveNoops: true, MergeCollinear: true})
	if !strings.ContainsAny(got, "aA") || strings.ContainsAny(got, "csCS") {
		t.Errorf("run with inner smooth cubic not fully converted: %q", got)
	}
	assertOnCircle(t, got, 50, 50, 50)
}

func TestConvertArcsRejectsNonCircular(t *testing.T) {
	// An ellipse-ish chain: no common circle, cubics stay.
	in := "M100 50C100 66 77.614 80 50 80C22.386 80 0 66 0 50C0 34 22.386 20 50 20C77.614 20 100 34 100 50Z"
	got := opt(t, in, Options{Precision: 3, RemoveNoops: true, MergeCollinear: true})
	if strings.ContainsAny(got, "aA") {
		t.Errorf("arc emitted for non-circular chain: %q", got)
	}
}

// assertOnCircle re-parses the output and samples every arc endpoint
// against the expected circle.
func assertOnCircle(t *testing.T, d string, cx, cy, r float64) {
	t.Helper()
	cs, err := Parse([]byte(d))
	if err != nil {
		t.Fatalf("re-parse %q: %v", d, err)
	}
	x, y := 0.0, 0.0
	for _, c := range cs {
		if len(c.Args) < 2 {
			continue
		}
		n := len(c.Args)
		nx, ny := c.Args[n-2], c.Args[n-1]
		if c.Op >= 'a' {
			nx, ny = x+nx, y+ny
		}
		x, y = nx, ny
		if math.Abs(math.Hypot(x-cx, y-cy)-r) > 0.01 {
			t.Errorf("endpoint (%v,%v) off circle by %v in %q", x, y,
				math.Hypot(x-cx, y-cy)-r, d)
		}
	}
}

func TestCloseVectorEmitsCleanNumbers(t *testing.T) {
	// The endpoint before z re-bases on the emitted start; the arithmetic
	// must never leak raw float64 text into the output.
	in := "M394.25 74.45l-3.1 1.2 6.35-1.45 4.5-1.25L394.25 74.45z"
	got := opt(t, in, Options{Precision: 3, RemoveNoops: true, MergeCollinear: true})
	if strings.Contains(got, "00000") || strings.Contains(got, "99999") {
		t.Errorf("raw float64 text in output: %q", got)
	}
}
