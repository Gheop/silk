package pass

import (
	"strings"
	"testing"

	"github.com/Gheop/silk/internal/dom"
)

func TestOptimizeGlyphPaths(t *testing.T) {
	in := `<svg><defs><font><font-face font-family="f"/><missing-glyph d="M 10 , 10 L 20 10 L 20 20"/><glyph unicode="a" d="M 100.0 200.0 C 100 300 200 300 200.0 200.0"/></font></defs><text font-family="f">a</text></svg>`
	doc := parse(t, in)
	cache := NewPathCache()
	OptimizePaths(doc, 3, cache)
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
	OptimizePaths(doc, 3, NewPathCache())
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
	OptimizePaths(doc, 3, NewPathCache())
	out := string(dom.Serialize(doc))
	if !strings.Contains(out, "4.16007 8.32") {
		t.Errorf("dashed path not at raised precision: %q", out)
	}
}

func TestScaledSubtreesKeepPrecision(t *testing.T) {
	// A ×200 cumulative transform turns a half-thousandth rounding into a
	// 0.1-unit displacement: precision follows the amplification.
	doc := parse(t, `<svg><g transform="scale(200)"><path d="M0.0016033 0.023L1.0004567 2.0001234"/></g></svg>`)
	OptimizePaths(doc, 3, NewPathCache())
	out := string(dom.Serialize(doc))
	if !strings.Contains(out, ".0016 ") && !strings.Contains(out, ".0016.") && !strings.Contains(out, ".0016L") {
		t.Errorf("scaled path rounded at base precision: %q", out)
	}
}
