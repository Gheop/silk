package pass

import (
	"strings"
	"testing"

	"github.com/Gheop/silk/internal/dom"
)

func runShapes(t *testing.T, in string) string {
	t.Helper()
	doc := parse(t, in)
	ConvertShapes(doc, Analyze(doc))
	return string(dom.Serialize(doc))
}

func TestConvertShapes(t *testing.T) {
	cases := []struct{ in, want string }{
		{`<svg><line x1="10" y1="0" x2="10" y2="50" stroke="red"/></svg>`,
			`<svg><path stroke="red" d="M10 0L10 50"/></svg>`},
		// Missing coordinates default to zero.
		{`<svg><line x2="5" y2="5" stroke="red"/></svg>`,
			`<svg><path stroke="red" d="M0 0L5 5"/></svg>`},
		{`<svg><rect x="1" y="2" width="30" height="40"/></svg>`,
			`<svg><path d="M1 2h30v40h-30z"/></svg>`},
		{`<svg><polygon points="1,2 3,4 5 6"/></svg>`,
			`<svg><path d="M1 2 3 4 5 6z"/></svg>`},
		{`<svg><polyline points="1 2 3 4"/></svg>`,
			`<svg><path d="M1 2 3 4"/></svg>`},
		// Children move along with the renamed element.
		{`<svg><rect width="3" height="4"><title>t</title></rect></svg>`,
			`<svg><path d="M0 0h3v4h-3z"><title>t</title></path></svg>`},
		// Rounded rects, units, zero sizes, odd point counts: untouched.
		{`<svg><rect width="30" height="40" rx="3"/></svg>`,
			`<svg><rect width="30" height="40" rx="3"/></svg>`},
		{`<svg><line x1="10%" y1="0" x2="1" y2="1"/></svg>`,
			`<svg><line x1="10%" y1="0" x2="1" y2="1"/></svg>`},
		{`<svg><rect width="0" height="40"/></svg>`,
			`<svg><rect width="0" height="40"/></svg>`},
		{`<svg><polygon points="1 2 3"/></svg>`,
			`<svg><polygon points="1 2 3"/></svg>`},
		// A stylesheet can match by element type: everything stays.
		{`<svg><style>line{stroke:red}</style><line x2="5" y2="5"/></svg>`,
			`<svg><style>line{stroke:red}</style><line x2="5" y2="5"/></svg>`},
	}
	for _, tc := range cases {
		if got := runShapes(t, tc.in); got != tc.want {
			t.Errorf("ConvertShapes(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
		}
	}
}

func TestConvertShapesKeepsPrefix(t *testing.T) {
	// A prefixed shape resolves its namespace through the prefix; dropping
	// it on rename would rebind the element to the in-scope default.
	in := `<svg xmlns="http://www.w3.org/2000/svg"><s:g xmlns="urn:x" xmlns:s="http://www.w3.org/2000/svg"><s:rect x="1" y="1" width="2" height="3"/></s:g></svg>`
	got := runShapes(t, in)
	if !strings.Contains(got, "<s:path") {
		t.Errorf("prefix lost on shape conversion: %q", got)
	}
}
