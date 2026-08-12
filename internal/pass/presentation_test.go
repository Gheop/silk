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
		// stroke-width is never rounded (miter amplification): the near-1
		// value stays and therefore cannot drop as a default.
		{`<path stroke="red" stroke-width="0.99999994" d="M0 0"/>`,
			`<path stroke="red" stroke-width=".99999994" d="M0 0"/>`},
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

func TestInitialValueDeclKeptWhenMaskingAttr(t *testing.T) {
	cases := []struct{ in, want string }{
		// The declaration is at fill's initial value but shadows a different
		// attribute: it must not vanish and leave the attribute in charge.
		// It overwrites the attribute, which then drops as a true default —
		// the element renders black (initial fill) as it always did.
		{`<path fill="#a12821" style="fill:#000000" d="M0 0"/>`,
			`<path d="M0 0"/>`},
		// Same for a non-inherited property.
		{`<path opacity=".5" style="opacity:1" d="M0 0"/>`,
			`<path d="M0 0"/>`},
		// Attribute and declaration agree: both are droppable defaults.
		{`<path fill="black" style="fill:#000000" d="M0 0"/>`,
			`<path d="M0 0"/>`},
	}
	for _, tc := range cases {
		in := `<svg>` + tc.in + `</svg>`
		want := `<svg>` + tc.want + `</svg>`
		if got := runPresentation(t, in, 3); got != want {
			t.Errorf("masking decl(%q)\n got: %q\nwant: %q", tc.in, got, want)
		}
	}
}

func TestInitialValueDeclKeptUnderStylesheet(t *testing.T) {
	// A stylesheet rule is outranked by the style attribute, so even an
	// initial-value declaration is load-bearing while a stylesheet exists.
	in := `<svg><style>path{opacity:.5}</style><path style="opacity:1" d="M0 0"/></svg>`
	if got := runPresentation(t, in, 3); !strings.Contains(got, "opacity:1") {
		t.Errorf("initial-value decl dropped under stylesheet: %q", got)
	}
}

func TestShorthandPinsLonghandsInStyle(t *testing.T) {
	// marker (CSS shorthand, no attribute form) stays in style; marker-end
	// after it overrides it there. Extracted to an attribute, marker-end
	// would lose to the style — so it must stay in the style too.
	in := `<path style="marker:none;marker-end:url(#a)" d="M0 0h9"/>`
	got := runPresentation(t, `<svg>`+in+`</svg>`, 3)
	if !strings.Contains(got, `style="marker:none;marker-end:url(#a)"`) {
		t.Errorf("marker-end left its shorthand behind: %q", got)
	}
	// Same family via font; unrelated props still convert.
	in2 := `<path style="font:10px serif;font-size:12px;fill:red" d="M0 0h9"/>`
	got2 := runPresentation(t, `<svg>`+in2+`</svg>`, 3)
	if !strings.Contains(got2, `font:10px serif;font-size:12px`) || !strings.Contains(got2, `fill="red"`) {
		t.Errorf("font family handling wrong: %q", got2)
	}
}

func TestStrokeWidthNeverRounded(t *testing.T) {
	// Miter joins at needle-sharp corners amplify a stroke-width error
	// without bound (miter length is width/sin(θ/2)): reformat exactly.
	cases := []struct{ in, want string }{
		{`<path stroke="red" stroke-width="1.04027808" d="M0 0h9"/>`,
			`<path stroke="red" stroke-width="1.04027808" d="M0 0h9"/>`},
		{`<path style="stroke:red;stroke-width:1.04027808" d="M0 0h9"/>`,
			`<path d="M0 0h9" stroke="red" stroke-width="1.04027808"/>`},
		{`<path stroke="red" stroke-width="0.500" d="M0 0h9"/>`,
			`<path stroke="red" stroke-width=".5" d="M0 0h9"/>`},
	}
	for _, tc := range cases {
		in := `<svg>` + tc.in + `</svg>`
		want := `<svg>` + tc.want + `</svg>`
		if got := runPresentation(t, in, 3); got != want {
			t.Errorf("stroke-width(%q)\n got: %q\nwant: %q", tc.in, got, want)
		}
	}
}

func TestNonCanonicalPropertyCasingUntouched(t *testing.T) {
	// CSS property names are case-insensitive on paper, but some renderers
	// only recognize lowercase: normalizing the author's casing changes
	// what they apply.
	in := `<svg><circle fill="red" style="FiLl: oRaNgE" r="5"/></svg>`
	if got := runPresentation(t, in, 3); !strings.Contains(got, "FiLl:oRaNgE") {
		t.Errorf("non-canonical casing rewritten: %q", got)
	}
}

func TestNegativeDasharrayUntouched(t *testing.T) {
	// A negative value (even -0) invalidates the whole dasharray and the
	// stroke renders solid; normalizing the sign away would validate the
	// list and turn the dashes on.
	in := `<svg><path stroke="red" stroke-dasharray="0.5842828517655089 -0" d="M0 0h9"/></svg>`
	if got := runPresentation(t, in, 3); !strings.Contains(got, `stroke-dasharray="0.5842828517655089 -0"`) {
		t.Errorf("negative dasharray rewritten: %q", got)
	}
}
