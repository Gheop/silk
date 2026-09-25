package pass

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Gheop/silk/internal/dom"
)

func runGroups(t *testing.T, in string) string {
	t.Helper()
	doc := parse(t, in)
	CollapseGroups(doc, Analyze(doc))
	return string(dom.Serialize(doc))
}

func TestCollapseGroups(t *testing.T) {
	cases := []struct{ in, want string }{
		// Attribute-less groups unwrap, recursively.
		{`<svg><g><g><path d="M0 0"/></g><rect/></g></svg>`,
			`<svg><path d="M0 0"/><rect/></svg>`},
		// Inheritable attributes push onto a lone child.
		{`<svg><g fill="red"><path d="M0 0"/></g></svg>`,
			`<svg><path d="M0 0" fill="red"/></svg>`},
		// The child's own value wins; the group's masked one is dropped.
		{`<svg><g fill="red"><path d="M0 0" fill="blue"/></g></svg>`,
			`<svg><path d="M0 0" fill="blue"/></svg>`},
		// "inherit" on the child depends on the group value: keep the group.
		{`<svg><g fill="red"><path d="M0 0" fill="inherit"/></g></svg>`,
			`<svg><g fill="red"><path d="M0 0" fill="inherit"/></g></svg>`},
		// Transforms concatenate, parent first.
		{`<svg><g transform="translate(1 2)"><path d="M0 0" transform="scale(3)"/></g></svg>`,
			`<svg><path d="M0 0" transform="translate(1 2) scale(3)"/></svg>`},
		// clip-path, mask, filter, style: the group stays.
		{`<svg><g clip-path="url(#c)"><path d="M0 0"/></g><clipPath id="c"/></svg>`,
			`<svg><g clip-path="url(#c)"><path d="M0 0"/></g><clipPath id="c"/></svg>`},
		{`<svg><g style="fill:red"><path d="M0 0"/></g></svg>`,
			`<svg><g style="fill:red"><path d="M0 0"/></g></svg>`},
		// A referenced group stays; an id alone also blocks unwrapping.
		{`<svg><g id="k"><path d="M0 0"/></g><use href="#k"/></svg>`,
			`<svg><g id="k"><path d="M0 0"/></g><use href="#k"/></svg>`},
		// Multiple children: attributes cannot push, but an attribute-less
		// group still unwraps.
		{`<svg><g fill="red"><path d="M0 0"/><path d="M1 1"/></g></svg>`,
			`<svg><g fill="red"><path d="M0 0"/><path d="M1 1"/></g></svg>`},
		// A stylesheet disables the whole pass.
		{`<svg><style>g{}</style><g><path d="M0 0"/></g></svg>`,
			`<svg><style>g{}</style><g><path d="M0 0"/></g></svg>`},
		// A referenced child does not accept pushed attributes.
		{`<svg><g fill="red"><path id="p" d="M0 0"/></g><use href="#p"/></svg>`,
			`<svg><g fill="red"><path id="p" d="M0 0"/></g><use href="#p"/></svg>`},
		// Group opacity moves onto a lone child without its own.
		{`<svg><g opacity=".5"><path d="M0 0"/></g></svg>`,
			`<svg><path d="M0 0" opacity=".5"/></svg>`},
		{`<svg><g opacity=".5"><path d="M0 0" opacity=".7"/></g></svg>`,
			`<svg><g opacity=".5"><path d="M0 0" opacity=".7"/></g></svg>`},
	}
	for _, tc := range cases {
		if got := runGroups(t, tc.in); got != tc.want {
			t.Errorf("CollapseGroups(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
		}
	}
}

func TestCollapseGroupsKeepsSwitchChildren(t *testing.T) {
	// <switch> renders only its first eligible child: unwrapping a child
	// group would promote its content to competing switch alternatives.
	in := `<svg><switch><g><path d="M0 0"/><path d="M9 9"/></g></switch></svg>`
	if got := runGroups(t, in); got != in {
		t.Errorf("switch child collapsed:\n got: %q\nwant: %q", got, in)
	}
	// Nested below a switch child, collapsing is fine again.
	in2 := `<svg><switch><g systemLanguage="fr"><g><path d="M0 0"/></g></g></switch></svg>`
	want2 := `<svg><switch><g systemLanguage="fr"><path d="M0 0"/></g></switch></svg>`
	if got := runGroups(t, in2); got != want2 {
		t.Errorf("nested collapse under switch child:\n got: %q\nwant: %q", got, want2)
	}
}

func TestCollapseKeepsTransformOffOriginCarriers(t *testing.T) {
	// transform-origin is inert on an element with no transform of its own;
	// pushing the group's transform down would activate it.
	in := `<svg><g transform="scale(2)"><rect transform-origin="bottom right" width="20" height="20"/></g></svg>`
	if got := runGroups(t, in); got != in {
		t.Errorf("transform pushed onto transform-origin carrier:\n got: %q", got)
	}
}

func TestCollapseGroupsKeepsGroupInClipPath(t *testing.T) {
	// A <g> inside <clipPath> is ignored by renderers (the clip is empty);
	// unwrapping its shapes would make the clip visible.
	in := `<svg><clipPath id="c"><g><path d="M0 0h1v1z"/></g></clipPath><path clip-path="url(#c)" d="M0 0h9v9z"/></svg>`
	if got := runGroups(t, in); got != in {
		t.Errorf("group in clipPath collapsed:\n got: %q\nwant: %q", got, in)
	}
}

func TestCollapseGroupsLinear(t *testing.T) {
	// Fifty thousand sibling groups used to cost a splice each (11 s).
	var sb strings.Builder
	sb.WriteString("<svg>")
	for i := range 50000 {
		fmt.Fprintf(&sb, `<g><path d="M%d 0h1"/></g>`, i)
	}
	sb.WriteString("</svg>")
	start := time.Now()
	doc := parse(t, sb.String())
	CollapseGroups(doc, Analyze(doc))
	if el := time.Since(start); el > 3*time.Second {
		t.Errorf("50k groups took %v: collapsing is not linear", el)
	}
	if n := len(doc.Children[0].Children); n != 50000 {
		t.Errorf("got %d children after collapsing, want 50000", n)
	}
	for _, c := range doc.Children[0].Children {
		if c.Parent != doc.Children[0] || c.Name != "path" {
			t.Fatalf("child %s with wrong parent or name", c.Name)
		}
	}
	// Nesting still collapses fully in one call.
	got := runGroups(t, `<svg><g><g><g fill="red"><path d="M0 0"/></g></g></g></svg>`)
	if want := `<svg><path d="M0 0" fill="red"/></svg>`; got != want {
		t.Errorf("nested collapse:\n got: %q\nwant: %q", got, want)
	}
}
