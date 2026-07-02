package pass

import (
	"testing"

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
