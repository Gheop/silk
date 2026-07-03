package pass

import (
	"strings"
	"testing"

	"github.com/Gheop/silk/internal/dom"
)

func runPresentation(t *testing.T, in string, prec int) string {
	t.Helper()
	doc := parse(t, in)
	OptimizePresentation(doc, Analyze(doc), prec)
	return string(dom.Serialize(doc))
}

func TestStyleToAttrs(t *testing.T) {
	cases := []struct{ in, want string }{
		// Recognized presentation properties become (shorter) attributes.
		{`<path style="fill:red;stroke:none" d="M0 0"/>`,
			`<path d="M0 0" fill="red"/>`},
		// Unknown properties stay in style; known ones move out.
		{`<path style="fill:red;-inkscape-font-specification:Sans" d="M0 0"/>`,
			`<path style="-inkscape-font-specification:Sans" d="M0 0" fill="red"/>`},
		// A style-set property overrides an existing attribute: the dead
		// attribute value is replaced.
		{`<path fill="blue" style="fill:red" d="M0 0"/>`,
			`<path fill="red" d="M0 0"/>`},
		// !important is left alone entirely.
		{`<path style="fill:red !important" d="M0 0"/>`,
			`<path style="fill:red !important" d="M0 0"/>`},
		// Unparseable css stays untouched.
		{`<path style="fill:url(&quot;#p&quot;)" d="M0 0"/>`,
			`<path style="fill:url(&quot;#p&quot;)" d="M0 0"/>`},
	}
	for _, tc := range cases {
		in := `<svg>` + tc.in + `</svg>`
		want := `<svg>` + tc.want + `</svg>`
		if got := runPresentation(t, in, 3); got != want {
			t.Errorf("style->attrs(%q)\n got: %q\nwant: %q", tc.in, got, want)
		}
	}
}

func TestStyleToAttrsBlockedByStylesheet(t *testing.T) {
	in := `<svg><style>path{fill:blue}</style><path style="fill:red" d="M0 0"/></svg>`
	// Inline style outranks the sheet; a presentation attribute would not.
	if got := runPresentation(t, in, 3); got != in {
		t.Errorf("stylesheet must block conversion:\n got: %q", got)
	}
}

func TestDropDefaults(t *testing.T) {
	cases := []struct{ in, want string }{
		// Non-inherited defaults always go.
		{`<path opacity="1" d="M0 0"/>`, `<path d="M0 0"/>`},
		// Inherited defaults go when no ancestor sets the property.
		{`<g><path fill-opacity="1" stroke="none" d="M0 0"/></g>`,
			`<g><path d="M0 0"/></g>`},
		// An ancestor setting the property keeps the child's default: it
		// was overriding the inherited value.
		{`<g fill-opacity=".5"><path fill-opacity="1" d="M0 0"/></g>`,
			`<g fill-opacity=".5"><path fill-opacity="1" d="M0 0"/></g>`},
		{`<g stroke="red"><path stroke="none" d="M0 0"/></g>`,
			`<g stroke="red"><path stroke="none" d="M0 0"/></g>`},
		// Inside style declarations too.
		{`<path style="fill-opacity:1;stroke-miterlimit:4;fill:#123456" d="M0 0"/>`,
			`<path d="M0 0" fill="#123456"/>`},
	}
	for _, tc := range cases {
		in := `<svg>` + tc.in + `</svg>`
		want := `<svg>` + tc.want + `</svg>`
		if got := runPresentation(t, in, 3); got != want {
			t.Errorf("defaults(%q)\n got: %q\nwant: %q", tc.in, got, want)
		}
	}
}

func TestDropDefaultsBlockedByUse(t *testing.T) {
	// Through <use>, an ancestor at the use site could set the inherited
	// property: the local default was meaningful.
	in := `<svg><g fill-opacity=".5"><use href="#p"/></g><path id="p" fill-opacity="1" d="M0 0"/></svg>`
	if got := runPresentation(t, in, 3); got != in {
		t.Errorf("use must block inherited-default removal:\n got: %q", got)
	}
}

func TestShortenColors(t *testing.T) {
	cases := []struct{ in, want string }{
		{`<path fill="#ffcc00" d="M0 0"/>`, `<path fill="#fc0" d="M0 0"/>`},
		{`<path fill="#FFCC00" d="M0 0"/>`, `<path fill="#fc0" d="M0 0"/>`},
		{`<path fill="#ff0000" d="M0 0"/>`, `<path fill="red" d="M0 0"/>`},
		{`<path fill="rgb(255, 204, 0)" d="M0 0"/>`, `<path fill="#fc0" d="M0 0"/>`},
		{`<path fill="#123456" d="M0 0"/>`, `<path fill="#123456" d="M0 0"/>`},
		// Ties keep the input form.
		{`<path fill="blue" d="M0 0"/>`, `<path fill="blue" d="M0 0"/>`},
		{`<path fill="magenta" d="M0 0"/>`, `<path fill="#f0f" d="M0 0"/>`},
		{`<path fill="currentColor" d="M0 0"/>`, `<path fill="currentColor" d="M0 0"/>`},
		{`<path fill="url(#g)" d="M0 0"/>`, `<path fill="url(#g)" d="M0 0"/>`},
		// black is fill's default: dropped rather than shortened.
		{`<path style="stop-color:#ffffff;fill:black" d="M0 0"/>`,
			`<path d="M0 0" stop-color="#fff"/>`},
	}
	for _, tc := range cases {
		in := `<svg>` + tc.in + `</svg>`
		want := `<svg>` + tc.want + `</svg>`
		if got := runPresentation(t, in, 3); got != want {
			t.Errorf("colors(%q)\n got: %q\nwant: %q", tc.in, got, want)
		}
	}
}

func TestMinifyStylesheetText(t *testing.T) {
	cases := []struct{ in, want string }{
		// Whitespace runs collapse; declarations survive untouched.
		{"<svg><style>\n\t.a { fill: red; }\n\t.b { stroke: blue; }\n</style><path class=\"a\" d=\"M0 0\"/></svg>",
			`<svg><style>.a { fill: red; } .b { stroke: blue; }</style><path class="a" d="M0 0"/></svg>`},
		// Quotes could hide significant whitespace: hands off.
		{`<svg><style>.a { font-family: "My  Font"; }</style><path class="a" d="M0 0"/></svg>`,
			`<svg><style>.a { font-family: "My  Font"; }</style><path class="a" d="M0 0"/></svg>`},
	}
	for _, tc := range cases {
		if got := runPresentation(t, tc.in, 3); got != tc.want {
			t.Errorf("stylesheet(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
		}
	}
}

func TestStyleMinify(t *testing.T) {
	// With a stylesheet present, conversion is off but pure minification of
	// the style value is still safe.
	in := `<svg><style>p{}</style><path style=" fill : red ; stroke : none ; " d="M0 0"/></svg>`
	want := `<svg><style>p{}</style><path style="fill:red;stroke:none" d="M0 0"/></svg>`
	if got := runPresentation(t, in, 3); got != want {
		t.Errorf("minify:\n got: %q\nwant: %q", got, want)
	}
}

func TestRoundNumericAttrs(t *testing.T) {
	cases := []struct{ in, want string }{
		{`<rect x="10.123456" y="0.50000001" width="100.0004" height="20"/>`,
			`<rect x="10.123" y=".5" width="100" height="20"/>`},
		{`<circle cx="1.23456789" cy="-0.5" r="3.1400001"/>`,
			`<circle cx="1.235" cy="-.5" r="3.14"/>`},
		{`<polygon points="10.0001,20.0002 30.5,40.25 -1.5,-2.5"/>`,
			`<polygon points="10 20 30.5 40.25-1.5-2.5"/>`},
		{`<line x1="0.5" y1="1.50" x2="10px" y2="7"/>`,
			`<line x1=".5" y1="1.5" x2="10" y2="7"/>`},
		// Percent and unknown units stay untouched.
		{`<rect width="50%" height="10em"/>`, `<rect width="50%" height="10em"/>`},
		// Opacity-like numbers round too (also via style conversion).
		{`<path opacity="0.30000000000000004" d="M0 0"/>`, `<path opacity=".3" d="M0 0"/>`},
		{`<path stroke="red" stroke-width="0.99999994" d="M0 0"/>`,
			`<path stroke="red" d="M0 0"/>`},
		// stroke-dasharray reformats but never rounds: a period error
		// accumulates by the repeat count along the stroke.
		{`<path stroke="red" stroke-dasharray="1.0001, 2.0002" d="M0 0"/>`,
			`<path stroke="red" stroke-dasharray="1.0001 2.0002" d="M0 0"/>`},
		{`<path stroke="red" stroke-dasharray="0.500, 0.250" d="M0 0"/>`,
			`<path stroke="red" stroke-dasharray=".5 .25" d="M0 0"/>`},
		// The root svg is never touched.
		{`<svg width="210.0001mm"><rect width="5.00001"/></svg>`,
			`<svg width="210.0001mm"><rect width="5"/></svg>`},
	}
	for _, tc := range cases {
		in, want := tc.in, tc.want
		if !strings.HasPrefix(in, "<svg") {
			in, want = `<svg>`+in+`</svg>`, `<svg>`+want+`</svg>`
		}
		if got := runPresentation(t, in, 3); got != want {
			t.Errorf("round(%q)\n got: %q\nwant: %q", tc.in, got, want)
		}
	}
}
