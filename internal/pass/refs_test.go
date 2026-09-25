package pass

import (
	"strings"
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

func TestAnimationReferencesAndGuards(t *testing.T) {
	// Animated hrefs reference every value in turn.
	doc := parse(t, `<svg><defs><g id="a"/><g id="b"/></defs><use href="#a"><animate attributeName="href" values="#a;#b"/></use></svg>`)
	refs := Analyze(doc)
	if !refs.HasAnimation || !refs.UsedID("b") {
		t.Errorf("animated href value not collected: animation=%v b=%v", refs.HasAnimation, refs.UsedID("b"))
	}
	Cleanup(doc, refs)
	if got, want := string(dom.Serialize(doc)), `<svg><defs><g id="a"/><g id="b"/></defs><use href="#a"><animate attributeName="href" values="#a;#b"/></use></svg>`; got != want {
		t.Errorf("animated target pruned:\n got: %q\nwant: %q", got, want)
	}
	// A stroke animated on later shows zero-length segments with round
	// caps: they must survive, as must a default that masks an animated
	// inherited value.
	in := `<svg><g fill="red"><animate attributeName="fill" to="blue"/><path fill="black" d="M0 0h1"/></g><path stroke="none" stroke-linecap="round" stroke-width="10" d="M10 10L10 10M20 20L30 30"><animate attributeName="stroke" to="red"/></path></svg>`
	doc = parse(t, in)
	refs = Analyze(doc)
	OptimizePresentation(doc, refs, 3)
	OptimizePaths(doc, 3, NewPathCache())
	got := string(dom.Serialize(doc))
	if !strings.Contains(got, `fill="#000"`) && !strings.Contains(got, `fill="black"`) {
		t.Errorf("default masking an animated inherited fill dropped: %q", got)
	}
	if !strings.Contains(got, "M10 10") {
		t.Errorf("zero-length segment under a stroke animation dropped: %q", got)
	}
	// A marker animated on a path pins its coordinates exactly.
	in = `<svg><path d="M0 0L10 .00041234L10 0"><animate attributeName="marker-end" to="url(#m)"/></path><marker id="m"/></svg>`
	doc = parse(t, in)
	OptimizePaths(doc, 3, NewPathCache())
	if got := string(dom.Serialize(doc)); !strings.Contains(got, ".00041234") {
		t.Errorf("coordinates rounded under an animated marker: %q", got)
	}
}
