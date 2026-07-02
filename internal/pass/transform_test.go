package pass

import (
	"testing"

	"github.com/Gheop/silk/internal/dom"
)

func runTransforms(t *testing.T, in string, prec int) string {
	t.Helper()
	doc := parse(t, in)
	ConvertTransforms(doc, prec)
	return string(dom.Serialize(doc))
}

func TestConvertTransforms(t *testing.T) {
	cases := []struct {
		in, want string
		prec     int
	}{
		// Identity transforms drop.
		{`<g transform="translate(0,0)"/>`, `<g/>`, 5},
		{`<g transform="matrix(1,0,0,1,0,0)"/>`, `<g/>`, 5},
		{`<g transform="scale(1)"/>`, `<g/>`, 5},
		// Lists collapse into one short form.
		{`<g transform="translate(10,20) translate(5,-3)"/>`, `<g transform="translate(15 17)"/>`, 5},
		{`<g transform="scale(2) scale(3,4)"/>`, `<g transform="scale(6 8)"/>`, 5},
		{`<g transform="translate(10) scale(2)"/>`, `<g transform="matrix(2 0 0 2 10 0)"/>`, 5},
		// A lone y-less translate stays short; zero y is omitted.
		{`<g transform="translate(10 0)"/>`, `<g transform="translate(10)"/>`, 5},
		{`<g transform="scale(3 3)"/>`, `<g transform="scale(3)"/>`, 5},
		// A pure rotation would need a long matrix: the original wins.
		{`<g transform="rotate(45)"/>`, `<g transform="rotate(45)"/>`, 5},
		// Translation components round to the transform precision; matrix
		// factors stay exact.
		{`<g transform="translate(10.123456,0.7) scale(1.5)"/>`, `<g transform="matrix(1.5 0 0 1.5 10.12346 .7)"/>`, 5},
		// Unparseable transforms stay untouched.
		{`<g transform="rotate(45deg)"/>`, `<g transform="rotate(45deg)"/>`, 5},
		{`<g transform="frobnicate(3)"/>`, `<g transform="frobnicate(3)"/>`, 5},
		// gradientTransform gets the same treatment.
		{`<linearGradient gradientTransform="translate(0)"/>`, `<linearGradient/>`, 5},
	}
	for _, tc := range cases {
		in := `<svg>` + tc.in + `</svg>`
		want := `<svg>` + tc.want + `</svg>`
		if got := runTransforms(t, in, tc.prec); got != want {
			t.Errorf("ConvertTransforms(%q)\n got: %q\nwant: %q", tc.in, got, want)
		}
	}
}
