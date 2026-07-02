package dom

import (
	"bytes"
	"testing"
)

// Verbatim round-trip: Serialize(Parse(x)) must equal x for any well-formed
// document, no matter how odd the formatting.
func TestRoundTripVerbatim(t *testing.T) {
	cases := []string{
		`<svg><path d="M0 0"/></svg>`,
		`<a b='x' c="y"/>`,
		"<a  b = \"x\"\n\tc='y' ></a >",
		"<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE svg PUBLIC \"-//W3C//DTD SVG 1.1//EN\" \"http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd\">\n<svg/>",
		`<svg><!-- a comment --><![CDATA[ raw <stuff> ]]><g/></svg>`,
		"<svg><path d=\"M0 0\n\tL10 10\"/></svg>", // newline and tab inside attr value
		`<a b="&amp;&#65;&quot;"/>`,
		"\xEF\xBB\xBF<svg/>\n",
		`<svg xmlns:inkscape="http://www.inkscape.org/namespaces/inkscape" inkscape:version="1.0"><inkscape:grid/></svg>`,
		`<a b=c/>`,
		`<svg><text> keep  spacing </text><g><g><path d=""/></g></g></svg>`,
		`<svg><style>.a { fill: url(#p); }</style><rect class="a"/></svg>`,
		"<!DOCTYPE svg [<!ENTITY foo \"bar\">]><svg a=\"&foo;\"/>",
		`<?xml-stylesheet type="text/css" href="style.css"?><svg/>`,
	}
	for _, c := range cases {
		doc, err := Parse([]byte(c))
		if err != nil {
			t.Errorf("Parse(%q): %v", c, err)
			continue
		}
		got := Serialize(doc)
		if !bytes.Equal(got, []byte(c)) {
			t.Errorf("round-trip mismatch\n in: %q\nout: %q", c, got)
		}
	}
}

func TestParseDoesNotMutateInput(t *testing.T) {
	// The tdewolff lexer normalizes whitespace inside attribute values in its
	// buffer; Parse must operate on a private copy.
	in := []byte("<a b=\"x\ny\"/>")
	orig := append([]byte(nil), in...)
	if _, err := Parse(in); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(in, orig) {
		t.Errorf("input mutated: %q", in)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []string{
		`<a></b>`,
		`</a>`,
		`<a><b></a>`,
		`<a>`,
		`<a b="x">`,
		``,
		`   `,
		`plain text only`,
	}
	for _, c := range cases {
		if _, err := Parse([]byte(c)); err == nil {
			t.Errorf("Parse(%q): expected error", c)
		}
	}
}

func TestAttrValueDecoding(t *testing.T) {
	doc, err := Parse([]byte(`<a b="&amp;&#65;&#x42;" c='q"q' d="x&unknown;y"/>`))
	if err != nil {
		t.Fatal(err)
	}
	el := doc.Children[0]
	if v, ok := el.AttrValue("b"); !ok || v != "&AB" {
		t.Errorf("b = %q, %v", v, ok)
	}
	if v, ok := el.AttrValue("c"); !ok || v != `q"q` {
		t.Errorf("c = %q, %v", v, ok)
	}
	// Unknown entity: value is opaque, reported as not-cleanly-decoded.
	if _, ok := el.AttrValue("d"); ok {
		t.Error("d: unknown entity should make the value opaque")
	}
}

func TestSetAttrPreservesSiblings(t *testing.T) {
	doc, err := Parse([]byte("<svg><path  fill = 'red'\n d=\"M0 0\"/></svg>"))
	if err != nil {
		t.Fatal(err)
	}
	p := doc.Children[0].Children[0]
	p.SetAttr("d", "M5 5")
	got := string(Serialize(doc))
	// The modified attribute is emitted canonically (single leading space);
	// its untouched sibling keeps the original spelling.
	want := "<svg><path  fill = 'red' d=\"M5 5\"/></svg>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSetAttrEscapes(t *testing.T) {
	doc, err := Parse([]byte(`<a b="x"/>`))
	if err != nil {
		t.Fatal(err)
	}
	doc.Children[0].SetAttr("b", `a&b<c"d`)
	got := string(Serialize(doc))
	want := `<a b="a&amp;b&lt;c&quot;d"/>`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRemoveAttrAndChild(t *testing.T) {
	doc, err := Parse([]byte(`<svg a="1" b="2"><g/><path d="M0 0"/><metadata>junk</metadata></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	root := doc.Children[0]
	root.RemoveAttr("a")
	root.RemoveChild(root.Children[2]) // metadata
	got := string(Serialize(doc))
	want := `<svg b="2"><g/><path d="M0 0"/></svg>`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReplaceWithChildren(t *testing.T) {
	doc, err := Parse([]byte(`<svg><g><path d="M0 0"/><rect/></g></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	g := doc.Children[0].Children[0]
	g.ReplaceWithChildren()
	got := string(Serialize(doc))
	want := `<svg><path d="M0 0"/><rect/></svg>`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDepthLimit(t *testing.T) {
	var b bytes.Buffer
	for range 20000 {
		b.WriteString("<g>")
	}
	if _, err := Parse(b.Bytes()); err == nil {
		t.Error("expected depth-limit error")
	}
}

func TestWalk(t *testing.T) {
	doc, err := Parse([]byte(`<svg><g><path/></g><rect/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	doc.Walk(func(n *Node) bool {
		if n.Kind == KindElement {
			names = append(names, n.Name)
		}
		return true
	})
	want := []string{"svg", "g", "path", "rect"}
	if len(names) != len(want) {
		t.Fatalf("walk order = %v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("walk order = %v, want %v", names, want)
		}
	}
}
