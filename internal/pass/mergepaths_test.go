package pass

import (
	"testing"

	"github.com/Gheop/silk/internal/dom"
)

func runMerge(t *testing.T, in string) string {
	t.Helper()
	doc := parse(t, in)
	MergePaths(doc, Analyze(doc), -1, NewPathCache())
	return string(dom.Serialize(doc))
}

func TestMergePaths(t *testing.T) {
	cases := []struct{ in, want string }{
		// Disjoint fills with identical attributes merge; a leading relative
		// moveto in the second path is absolute by definition and stays so.
		{`<svg><path fill="red" d="M0 0h1v1H0z"/><path fill="red" d="m10 10h1v1h-1z"/></svg>`,
			`<svg><path fill="red" d="M0 0h1v1H0zM10 10h1v1h-1z"/></svg>`},
		// Chains collapse into one.
		{`<svg><path d="M0 0h1"/><path d="M10 0h1"/><path d="M20 0h1"/></svg>`,
			`<svg><path d="M0 0h1M10 0h1M20 0h1"/></svg>`},
		// Different attributes: no merge.
		{`<svg><path fill="red" d="M0 0h1"/><path fill="blue" d="M10 0h1"/></svg>`,
			`<svg><path fill="red" d="M0 0h1"/><path fill="blue" d="M10 0h1"/></svg>`},
		// An intervening painted element breaks adjacency.
		{`<svg><path d="M0 0h1"/><rect/><path d="M10 0h1"/></svg>`,
			`<svg><path d="M0 0h1"/><rect/><path d="M10 0h1"/></svg>`},
		// An id blocks the merge.
		{`<svg><path id="a" d="M0 0h1"/><path d="M10 0h1"/></svg>`,
			`<svg><path id="a" d="M0 0h1"/><path d="M10 0h1"/></svg>`},
		// url() references block the merge (bounding-box-relative units).
		{`<svg><path fill="url(#g)" d="M0 0h1"/><path fill="url(#g)" d="M10 0h1"/></svg>`,
			`<svg><path fill="url(#g)" d="M0 0h1"/><path fill="url(#g)" d="M10 0h1"/></svg>`},
		// Overlapping evenodd fills must not merge: overlap becomes a hole.
		{`<svg><path fill-rule="evenodd" d="M0 0h10v10H0z"/><path fill-rule="evenodd" d="M5 5h10v10H5z"/></svg>`,
			`<svg><path fill-rule="evenodd" d="M0 0h10v10H0z"/><path fill-rule="evenodd" d="M5 5h10v10H5z"/></svg>`},
		// Disjoint evenodd is fine.
		{`<svg><path fill-rule="evenodd" d="M0 0h1v1H0z"/><path fill-rule="evenodd" d="M10 10h1v1h-1z"/></svg>`,
			`<svg><path fill-rule="evenodd" d="M0 0h1v1H0zM10 10h1v1h-1z"/></svg>`},
		// Overlapping strokes must not merge (paint order changes).
		{`<svg><path stroke="red" d="M0 0h10v10H0z"/><path stroke="red" d="M5 5h10v10H5z"/></svg>`,
			`<svg><path stroke="red" d="M0 0h10v10H0z"/><path stroke="red" d="M5 5h10v10H5z"/></svg>`},
		// Nearby strokes: bounding boxes inflate by the stroke; too close
		// does not merge, far enough does.
		{`<svg><path stroke="red" stroke-width="4" d="M0 0h10"/><path stroke="red" stroke-width="4" d="M0 3h10"/></svg>`,
			`<svg><path stroke="red" stroke-width="4" d="M0 0h10"/><path stroke="red" stroke-width="4" d="M0 3h10"/></svg>`},
		{`<svg><path stroke="red" stroke-width="4" d="M0 0h10"/><path stroke="red" stroke-width="4" d="M0 50h10"/></svg>`,
			`<svg><path stroke="red" stroke-width="4" d="M0 0h10M0 50h10"/></svg>`},
		// Partial opacity with overlap: no merge (double-painting shows).
		{`<svg><path fill-opacity=".5" d="M0 0h10v10H0z"/><path fill-opacity=".5" d="M5 5h10v10H5z"/></svg>`,
			`<svg><path fill-opacity=".5" d="M0 0h10v10H0z"/><path fill-opacity=".5" d="M5 5h10v10H5z"/></svg>`},
		// A stylesheet disables the pass.
		{`<svg><style>path{}</style><path d="M0 0h1"/><path d="M10 0h1"/></svg>`,
			`<svg><style>path{}</style><path d="M0 0h1"/><path d="M10 0h1"/></svg>`},
		// Markers block the merge (vertices change).
		{`<svg><path marker-mid="url(#m)" d="M0 0h1"/><path marker-mid="url(#m)" d="M10 0h1"/></svg>`,
			`<svg><path marker-mid="url(#m)" d="M0 0h1"/><path marker-mid="url(#m)" d="M10 0h1"/></svg>`},
	}
	for _, tc := range cases {
		if got := runMerge(t, tc.in); got != tc.want {
			t.Errorf("MergePaths(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
		}
	}
}
