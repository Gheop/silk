// Package fidelity proves optimizations correct by rendering: original and
// optimized documents are rasterized with resvg and compared pixel by pixel.
// The renderer is a test-only dependency; tests skip cleanly when absent.
package fidelity

import (
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// renderWidth is fixed so results are comparable across files and runs.
const renderWidth = 512

// Tolerance: rounding coordinates shifts anti-aliased edges by a fraction of
// a pixel, and stroke joins on line art occasionally flip an isolated pixel
// entirely. A real geometry defect (a displaced or recolored shape) shows up
// as a cluster of strongly differing pixels, so the gate bounds how many
// pixels may differ at all and, much tighter, how many may differ strongly.
// For scale: svgo at its default precision exceeds these bounds on the same
// corpus files.
const (
	softDiff      = 8      // per-channel difference ignored entirely
	strongDiff    = 64     // per-channel difference considered strong
	maxBadFrac    = 0.002  // pixels above softDiff
	maxStrongFrac = 0.0002 // pixels above strongDiff
)

// Result summarizes a pixel comparison.
type Result struct {
	MaxDiff      int // worst per-channel difference
	BadPixels    int // pixels with any channel above softDiff
	StrongPixels int // pixels with any channel above strongDiff
	TotalPixels  int
}

// Acceptable reports whether the difference is within the fidelity tolerance.
func (r Result) Acceptable() bool {
	return float64(r.BadPixels) <= maxBadFrac*float64(r.TotalPixels) &&
		float64(r.StrongPixels) <= maxStrongFrac*float64(r.TotalPixels)
}

func (r Result) String() string {
	return fmt.Sprintf("maxDiff=%d badPixels=%d strongPixels=%d total=%d",
		r.MaxDiff, r.BadPixels, r.StrongPixels, r.TotalPixels)
}

// ResvgPath returns the resvg binary path, or "" when unavailable.
func ResvgPath() string {
	p, err := exec.LookPath("resvg")
	if err != nil {
		return ""
	}
	return p
}

// RenderDiff rasterizes both documents and measures their pixel difference.
func RenderDiff(dir string, original, optimized []byte) (Result, error) {
	a, err := render(dir, "a", original)
	if err != nil {
		return Result{}, fmt.Errorf("render original: %w", err)
	}
	b, err := render(dir, "b", optimized)
	if err != nil {
		return Result{}, fmt.Errorf("render optimized: %w", err)
	}
	if a.Bounds() != b.Bounds() {
		return Result{}, fmt.Errorf("size mismatch: %v vs %v", a.Bounds(), b.Bounds())
	}
	res := Result{TotalPixels: a.Bounds().Dx() * a.Bounds().Dy()}
	for i := 0; i < len(a.Pix); i += 4 {
		m := 0
		for c := 0; c < 4; c++ {
			d := int(a.Pix[i+c]) - int(b.Pix[i+c])
			if d < 0 {
				d = -d
			}
			if d > m {
				m = d
			}
		}
		if m > res.MaxDiff {
			res.MaxDiff = m
		}
		if m > softDiff {
			res.BadPixels++
		}
		if m > strongDiff {
			res.StrongPixels++
		}
	}
	return res, nil
}

func render(dir, name string, svg []byte) (*image.NRGBA, error) {
	in := filepath.Join(dir, name+".svg")
	out := filepath.Join(dir, name+".png")
	if err := os.WriteFile(in, svg, 0o600); err != nil {
		return nil, err
	}
	cmd := exec.Command("resvg", "--width", fmt.Sprint(renderWidth), in, out)
	if msg, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("resvg: %v: %s", err, msg)
	}
	f, err := os.Open(out)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	n := image.NewNRGBA(img.Bounds())
	draw.Draw(n, img.Bounds(), img, img.Bounds().Min, draw.Src)
	return n, nil
}

// Compare fails t when optimized does not render identically to original
// within tolerance. It skips when resvg is not installed.
func Compare(t *testing.T, name string, original, optimized []byte) {
	t.Helper()
	if ResvgPath() == "" {
		t.Skip("resvg not installed; skipping fidelity check")
	}
	res, err := RenderDiff(t.TempDir(), original, optimized)
	if err != nil {
		t.Errorf("%s: %v", name, err)
		return
	}
	if !res.Acceptable() {
		t.Errorf("%s: render differs: %s", name, res)
	}
}
