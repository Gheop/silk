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
