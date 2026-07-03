package pass

import (
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
