package pass

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Gheop/silk/internal/dom"
)

func TestOptimizeGlyphPaths(t *testing.T) {
	in := `<svg><defs><font><font-face font-family="f"/><missing-glyph d="M 10 , 10 L 20 10 L 20 20"/><glyph unicode="a" d="M 100.0 200.0 C 100 300 200 300 200.0 200.0"/></font></defs><text font-family="f">a</text></svg>`
	doc := parse(t, in)
	cache := NewPathCache()
	OptimizePaths(doc, Analyze(doc), 3, cache)
	got := string(dom.Serialize(doc))
	want := `<svg><defs><font><font-face font-family="f"/><missing-glyph d="M10 10h10v10"/><glyph unicode="a" d="M100 200c0 100 100 100 100 0"/></font></defs><text font-family="f">a</text></svg>`
	if got != want {
		t.Errorf("OptimizePaths glyphs\n got: %q\nwant: %q", got, want)
	}
}

func TestMarkerPathsKeepExactCoordinates(t *testing.T) {
	// An orient="auto" marker rotates with vertex tangents that can be
	// shorter than any rounding residual: no finite precision bounds the
	// rotation, so marker-carrying paths are re-encoded exactly.
	doc := parse(t, `<svg><path marker-mid="url(#m)" d="M10.005 1L10.0049 1.000000063L20 2"/><marker id="m"><path d="M0 0h1v1z"/></marker></svg>`)
	OptimizePaths(doc, Analyze(doc), 3, NewPathCache())
	out := string(dom.Serialize(doc))
	if !strings.Contains(out, "10.0049 1.000000063") {
		t.Errorf("marker path rounded: %q", out)
	}
}

func TestDashedPathsKeepExactCoordinates(t *testing.T) {
	// Dash phase integrates the whole path length: geometry error
	// accumulates along a dashed stroke, so dashed paths get two extra
	// decimals of precision.
	doc := parse(t, `<svg><path stroke="red" stroke-dasharray="3.55 7.167" d="M0 0L4.160073040064538 8.320"/></svg>`)
	OptimizePaths(doc, Analyze(doc), 3, NewPathCache())
	out := string(dom.Serialize(doc))
	if !strings.Contains(out, "4.16007 8.32") {
		t.Errorf("dashed path not at raised precision: %q", out)
	}
}

func TestScaledSubtreesKeepPrecision(t *testing.T) {
	// A ×200 cumulative transform turns a half-thousandth rounding into a
	// 0.1-unit displacement: precision follows the amplification.
	doc := parse(t, `<svg><g transform="scale(200)"><path d="M0.0016033 0.023L1.0004567 2.0001234"/></g></svg>`)
	OptimizePaths(doc, Analyze(doc), 3, NewPathCache())
	out := string(dom.Serialize(doc))
	if !strings.Contains(out, ".0016 ") && !strings.Contains(out, ".0016.") && !strings.Contains(out, ".0016L") {
		t.Errorf("scaled path rounded at base precision: %q", out)
	}
}

func TestScaleBumpSeesNestedViewports(t *testing.T) {
	// A nested svg mapping a 1-unit viewBox onto 1000 px amplifies a
	// coordinate rounding a thousandfold: the precision follows.
	in := `<svg width="1000" height="1000"><svg viewBox="0 0 1 1" width="1000" height="1000"><path d="M.12345 .12345L.5 .5"/></svg></svg>`
	doc := parse(t, in)
	OptimizePaths(doc, Analyze(doc), 3, NewPathCache())
	if got := string(dom.Serialize(doc)); !strings.Contains(got, ".12345") {
		t.Errorf("coordinates rounded inside a magnifying nested viewport: %q", got)
	}
	// A CSS transform is a scale the scan cannot parse: exact.
	in = `<svg><g style="transform:scale(1000)"><path d="M.12345 .12345L.5 .5"/></g></svg>`
	doc = parse(t, in)
	OptimizePaths(doc, Analyze(doc), 3, NewPathCache())
	if got := string(dom.Serialize(doc)); !strings.Contains(got, ".12345") {
		t.Errorf("coordinates rounded under an inline CSS transform: %q", got)
	}
}

func TestPathOptionsLinearInSiblings(t *testing.T) {
	// The marker and dash guards walk ancestors for every path; looking at
	// each ancestor's children while doing so made a parent with tens of
	// thousands of shapes quadratic (a 1 MiB corpus file took 10 minutes).
	var sb strings.Builder
	sb.WriteString("<svg>")
	for i := range 40000 {
		fmt.Fprintf(&sb, `<path d="M%d 0h1"/>`, i)
	}
	sb.WriteString("</svg>")
	doc := parse(t, sb.String())
	start := time.Now()
	OptimizePaths(doc, Analyze(doc), 3, NewPathCache())
	if el := time.Since(start); el > 3*time.Second {
		t.Errorf("40k sibling paths took %v", el)
	}
}
