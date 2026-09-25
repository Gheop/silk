package pass

import (
	"testing"

	"github.com/Gheop/silk/internal/dom"
)

func parse(t *testing.T, s string) *dom.Node {
	t.Helper()
	doc, err := dom.Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestAnalyzeRefs(t *testing.T) {
	doc := parse(t, `<svg>
		<path fill="url(#grad)" clip-path="url( '#clip' )"/>
		<use href="#sym"/>
		<a xlink:href="#anchor"/>
		<g aria-labelledby="lbl other"/>
		<path style="mask:url(#m);fill:red"/>
	</svg>`)
	r := Analyze(doc)
	for _, id := range []string{"grad", "clip", "sym", "anchor", "lbl", "other", "m"} {
		if !r.UsedID(id) {
			t.Errorf("id %q should be referenced", id)
		}
	}
	if r.UsedID("nope") {
		t.Error("unreferenced id reported as used")
	}
	if !r.HasUse {
		t.Error("HasUse not set")
	}
	if r.HasStylesheet {
		t.Error("HasStylesheet wrongly set")
	}
}

func TestAnalyzeStylesheet(t *testing.T) {
	doc := parse(t, `<svg><style>.a { fill: url(#p); } #direct { stroke: red; }</style><rect class="a"/></svg>`)
	r := Analyze(doc)
	if !r.HasStylesheet {
		t.Error("HasStylesheet not set")
	}
	if !r.UsedID("p") || !r.UsedID("direct") {
		t.Error("stylesheet ids not collected")
	}
	// With a stylesheet everything counts as potentially referenced.
	if !r.UsedID("anything") {
		t.Error("stylesheet must make all ids used")
	}

	doc = parse(t, `<?xml-stylesheet href="a.css"?><svg/>`)
	if !Analyze(doc).HasStylesheet {
		t.Error("xml-stylesheet PI not detected")
	}
}

func TestStylesheetNonASCIIIDReference(t *testing.T) {
	// Turkish "Adsız degrade" (Illustrator's unnamed gradient) produces ids
	// beyond ASCII; the stylesheet scan must capture them whole.
	doc := parse(t, `<svg><style>.a { fill: url(#Adsız_degrade_17); }</style>`+
		`<defs><linearGradient id="Adsız_degrade_17"/></defs><path class="a" d="M0 0"/></svg>`)
	refs := Analyze(doc)
	if !refs.ConcretelyUsedID("Adsız_degrade_17") {
		t.Fatalf("non-ASCII id referenced from stylesheet not seen as used")
	}
}

func TestAnalyzeTrimsHref(t *testing.T) {
	// Browsers trim the URL before resolving the fragment.
	doc := parse(t, `<svg><use href=" #s "/></svg>`)
	if !Analyze(doc).UsedID("s") {
		t.Error("href with surrounding whitespace not collected")
	}
}

func TestScriptFreezesStructure(t *testing.T) {
	// Code can address any element by id and read any attribute, so a
	// script or an event handler disables defs pruning, group collapsing,
	// path merging and style-to-attribute rewriting alike.
	for _, in := range []string{
		`<svg><script>1</script><defs><g id="x"><path d="M0 0h1"/></g></defs><g><path class="k" style="fill:red" d="M0 0h1"/></g><path class="k" style="fill:red" d="M5 0h1"/></svg>`,
		`<svg onload="go()"><defs><g id="x"><path d="M0 0h1"/></g></defs><g><path class="k" style="fill:red" d="M0 0h1"/></g><path class="k" style="fill:red" d="M5 0h1"/></svg>`,
	} {
		doc := parse(t, in)
		refs := Analyze(doc)
		if !refs.HasScript {
			t.Fatalf("HasScript not set for %q", in)
		}
		Cleanup(doc, refs)
		OptimizePresentation(doc, refs, 3)
		CollapseGroups(doc, refs)
		ConvertShapes(doc, refs)
		MergePaths(doc, refs, 3, NewPathCache())
		if got := string(dom.Serialize(doc)); got != in {
			t.Errorf("structure changed under a script:\n got: %q\nwant: %q", got, in)
		}
	}
}
