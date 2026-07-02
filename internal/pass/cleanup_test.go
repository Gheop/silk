package pass

import (
	"testing"

	"github.com/Gheop/silk/internal/dom"
)

func runCleanup(t *testing.T, in string) string {
	t.Helper()
	doc := parse(t, in)
	Cleanup(doc, Analyze(doc))
	return string(dom.Serialize(doc))
}

func TestCleanup(t *testing.T) {
	cases := []struct{ in, want string }{
		// Comments and metadata go.
		{`<svg><!-- x --><metadata>junk</metadata><g><path d="M0 0"/></g></svg>`,
			`<svg><g><path d="M0 0"/></g></svg>`},
		// UTF-8 prolog and subset-free doctype go.
		{"<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE svg PUBLIC \"x\" \"y\">\n<svg/>",
			`<svg/>`},
		// A doctype with an internal subset stays (it can define entities).
		{`<!DOCTYPE svg [<!ENTITY a "b">]><svg c="&a;"/>`,
			`<!DOCTYPE svg [<!ENTITY a "b">]><svg c="&a;"/>`},
		// A non-UTF-8 prolog stays.
		{`<?xml version="1.0" encoding="ISO-8859-1"?><svg/>`,
			`<?xml version="1.0" encoding="ISO-8859-1"?><svg/>`},
		// Editor namespaces: elements, attributes, and the declaration.
		{`<svg xmlns="http://www.w3.org/2000/svg" xmlns:inkscape="http://www.inkscape.org/namespaces/inkscape" inkscape:version="1.0"><sodipodi:namedview xmlns:sodipodi="http://sodipodi.sourceforge.net/DTD/sodipodi-0.0.dtd" inkscape:cx="1"/><path inkscape:connector-curvature="0" d="M0 0"/></svg>`,
			`<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/></svg>`},
		// Whitespace between elements collapses, text content survives.
		{"<svg>\n  <g>\n    <path d=\"M0 0\"/>\n  </g>\n  <text> a <tspan> b </tspan></text>\n</svg>",
			`<svg><g><path d="M0 0"/></g><text> a <tspan> b </tspan></text></svg>`},
		// xml:space="preserve" protects whitespace below it.
		{`<svg xml:space="preserve"><g> <path d="M0 0"/> </g></svg>`,
			`<svg xml:space="preserve"><g> <path d="M0 0"/> </g></svg>`},
		// Empty containers, recursively.
		{`<svg><g><g></g><defs> </defs></g><path d="M0 0"/></svg>`,
			`<svg><path d="M0 0"/></svg>`},
		// Referenced empty containers stay.
		{`<svg><g id="e"/><use href="#e"/></svg>`,
			`<svg><g id="e"/><use href="#e"/></svg>`},
		// Empty container with a filter stays (feFlood can paint).
		{`<svg><g filter="url(#f)"/><filter id="f"/></svg>`,
			`<svg><g filter="url(#f)"/><filter id="f"/></svg>`},
		// Metadata whose subtree carries a referenced id stays.
		{`<svg><metadata><x id="keep"/></metadata><use href="#keep"/></svg>`,
			`<svg><metadata><x id="keep"/></metadata><use href="#keep"/></svg>`},
	}
	for _, tc := range cases {
		if got := runCleanup(t, tc.in); got != tc.want {
			t.Errorf("Cleanup(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
		}
	}
}

func TestRemoveUnreferencedDefs(t *testing.T) {
	cases := []struct{ in, want string }{
		// Unreferenced definitions go; referenced ones stay.
		{`<svg><defs><linearGradient id="used"/><linearGradient id="junk"/><path id="dead" d="M0 0h9000"/></defs><rect fill="url(#used)"/></svg>`,
			`<svg><defs><linearGradient id="used"/></defs><rect fill="url(#used)"/></svg>`},
		// Chains: A references B, A itself referenced.
		{`<svg><defs><linearGradient id="a" href="#b"/><linearGradient id="b"/></defs><rect fill="url(#a)"/></svg>`,
			`<svg><defs><linearGradient id="a" href="#b"/><linearGradient id="b"/></defs><rect fill="url(#a)"/></svg>`},
		// A stylesheet #id reference protects; style and font never leave.
		{`<svg><style>#s{stroke:red}</style><defs><path id="s" d="M0 0"/><font id="f"/></defs></svg>`,
			`<svg><style>#s{stroke:red}</style><defs><path id="s" d="M0 0"/><font id="f"/></defs></svg>`},
		// A fully unreferenced defs empties out and disappears.
		{`<svg><defs><clipPath id="c"><path d="M0 0h1"/></clipPath></defs><rect/></svg>`,
			`<svg><rect/></svg>`},
	}
	for _, tc := range cases {
		if got := runCleanup(t, tc.in); got != tc.want {
			t.Errorf("Cleanup(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
		}
	}
}
