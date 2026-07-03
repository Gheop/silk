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
		// xml:space="preserve" protects whitespace in text content; between
		// structural elements it never renders and goes regardless. Here the
		// text is too plain for preserve to matter, so the attribute goes too.
		{`<svg xml:space="preserve"><text>t</text><g> <path d="M0 0"/> </g></svg>`,
			`<svg><text>t</text><g><path d="M0 0"/></g></svg>`},
		// Whitespace-sensitive text keeps its own preserve; plain siblings
		// lose theirs.
		{`<svg><text xml:space="preserve"> a  b </text><text xml:space="preserve">c d</text></svg>`,
			`<svg><text xml:space="preserve"> a  b </text><text>c d</text></svg>`},
		// Under preserve, whitespace inside unrecognized elements stays.
		{`<svg xml:space="preserve"><text>t</text><unknown> <path d="M0 0"/> </unknown></svg>`,
			`<svg xml:space="preserve"><text>t</text><unknown> <path d="M0 0"/> </unknown></svg>`},
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

func TestRemoveRedundantNamespaces(t *testing.T) {
	cases := []struct{ in, want string }{
		// Re-declarations already in scope with the same URI go.
		{`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><use xmlns:xlink="http://www.w3.org/1999/xlink" xlink:href="#a"/><defs xmlns="http://www.w3.org/2000/svg"><path id="a" d="M0 0h1"/></defs></svg>`,
			`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><use xlink:href="#a"/><defs><path id="a" d="M0 0h1"/></defs></svg>`},
		// A prefix no name uses goes; a used one stays.
		{`<svg xmlns="http://www.w3.org/2000/svg" xmlns:svg="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><use xlink:href="#b"/><path id="b" d="M0 0h1"/></svg>`,
			`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><use xlink:href="#b"/><path id="b" d="M0 0h1"/></svg>`},
		// Re-declaration with a different URI stays and scopes its subtree.
		{`<svg xmlns="http://www.w3.org/2000/svg" xmlns:a="urn:one"><a:x/><g xmlns:a="urn:two"><a:x/><g xmlns:a="urn:two"><a:x/></g></g></svg>`,
			`<svg xmlns="http://www.w3.org/2000/svg" xmlns:a="urn:one"><a:x/><g xmlns:a="urn:two"><a:x/><g><a:x/></g></g></svg>`},
		// SMIL attributeName counts as a prefix use.
		{`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><animate attributeName="xlink:href"/></svg>`,
			`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><animate attributeName="xlink:href"/></svg>`},
		// A stylesheet resolves selector namespaces through its own
		// @namespace prefixes, so unused XML prefixes still go; a script
		// could look one up at runtime, so it keeps them all.
		{`<svg xmlns="http://www.w3.org/2000/svg" xmlns:q="urn:q"><style>rect{}</style><g xmlns:q="urn:q"><rect width="1" height="1"/></g></svg>`,
			`<svg xmlns="http://www.w3.org/2000/svg"><style>rect{}</style><g><rect width="1" height="1"/></g></svg>`},
		{`<svg xmlns="http://www.w3.org/2000/svg" xmlns:q="urn:q"><script>1</script><g xmlns:q="urn:q"><path d="M0 0h1"/></g></svg>`,
			`<svg xmlns="http://www.w3.org/2000/svg" xmlns:q="urn:q"><script>1</script><g><path d="M0 0h1"/></g></svg>`},
	}
	for _, tc := range cases {
		if got := runCleanup(t, tc.in); got != tc.want {
			t.Errorf("Cleanup(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
		}
	}
}

func TestRemoveInertSVGAttrs(t *testing.T) {
	cases := []struct{ in, want string }{
		// version and zero x/y are inert on svg elements, at any depth.
		{`<svg version="1.1" x="0px" y="0px" width="10" height="10"><svg x="0" y="5"><path d="M0 0h1"/></svg></svg>`,
			`<svg width="10" height="10"><svg y="5"><path d="M0 0h1"/></svg></svg>`},
		// Nonzero offsets stay put; other elements keep their x.
		{`<svg><rect x="0px" width="1" height="1"/></svg>`,
			`<svg><rect x="0px" width="1" height="1"/></svg>`},
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

func TestCanonicalizeTagsAndXMLSpace(t *testing.T) {
	cases := []struct{ in, want string }{
		// Editor indentation inside tags collapses to single spaces.
		{"<svg>\n  <path\n     fill=\"red\"\n     d=\"M0 0\"/>\n</svg>",
			`<svg><path fill="red" d="M0 0"/></svg>`},
		// xml:space goes when the document has no text content.
		{`<svg xml:space="preserve"><g> <path d="M0 0"/> </g></svg>`,
			`<svg><g><path d="M0 0"/></g></svg>`},
		// With text content it stays and keeps protecting that text, while
		// inter-element whitespace still goes.
		{`<svg xml:space="preserve"><text> a </text><g> <path d="M0 0"/> </g></svg>`,
			`<svg xml:space="preserve"><text> a </text><g><path d="M0 0"/></g></svg>`},
	}
	for _, tc := range cases {
		if got := runCleanup(t, tc.in); got != tc.want {
			t.Errorf("Cleanup(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
		}
	}
}
