package dom

import (
	"bytes"
	"strings"
	"testing"
	"time"
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

func TestParseUTF16(t *testing.T) {
	src := `<?xml version="1.0" encoding="UTF-16"?><svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0h20" fill="red"/></svg>`
	for name, enc := range map[string]func(rune) []byte{
		"LE": func(r rune) []byte { return []byte{byte(r), byte(r >> 8)} },
		"BE": func(r rune) []byte { return []byte{byte(r >> 8), byte(r)} },
	} {
		var b []byte
		if name == "LE" {
			b = append(b, 0xFF, 0xFE)
		} else {
			b = append(b, 0xFE, 0xFF)
		}
		for _, r := range src {
			b = append(b, enc(r)...)
		}
		doc, err := Parse(b)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out := string(Serialize(doc))
		if !strings.Contains(out, `encoding="UTF-8"`) || !strings.Contains(out, `d="M0 0h20"`) {
			t.Errorf("%s: bad transcode: %s", name, out)
		}
	}
}

func TestEntityExpansionIsBounded(t *testing.T) {
	// A 100 KB entity referenced 20 000 times would decode to 2 GB; the
	// value must stay opaque (raw kept) and the parse must stay cheap.
	value := strings.Repeat("x", 100<<10)
	refs := strings.Repeat("&e;", 20000)
	in := `<!DOCTYPE svg [<!ENTITY e "` + value + `">]><svg><path d="M0 0" data-x="` + refs + `"/></svg>`
	start := time.Now()
	doc, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Errorf("parse took %v: entity expansion is not bounded", el)
	}
	path := doc.Children[1].Children[0]
	if _, ok := path.AttrValue("data-x"); ok {
		t.Error("over-budget entity expansion decoded instead of staying opaque")
	}
	if got := string(Serialize(doc)); got != in {
		t.Error("document with an over-budget entity not kept verbatim")
	}
	// Within budget, references still resolve.
	small := `<!DOCTYPE svg [<!ENTITY ns "http://x">]><svg xmlns:a="&ns;"/>`
	doc, err = Parse([]byte(small))
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := doc.Children[1].AttrValue("xmlns:a"); !ok || v != "http://x" {
		t.Errorf("small entity not resolved: %q %v", v, ok)
	}
}

func TestSerializeKeepsWhitespaceReferences(t *testing.T) {
	// XML normalizes literal newlines and tabs in attribute values to
	// spaces, so a value carrying them (from &#10; and &#9;) must be
	// written back as references when the tag is re-serialized.
	doc, err := Parse([]byte("<svg\n  aria-label=\"a&#10;b&#9;c\"><path d=\"M0 0\"/></svg>"))
	if err != nil {
		t.Fatal(err)
	}
	doc.Children[0].CanonicalizeStartTag()
	got := string(Serialize(doc))
	if want := `<svg aria-label="a&#10;b&#9;c"><path d="M0 0"/></svg>`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	again, err := Parse([]byte(got))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := again.Children[0].AttrValue("aria-label"); v != "a\nb\tc" {
		t.Errorf("re-parsed value %q", v)
	}
}

func TestMalformedProcessingInstructionIsAnError(t *testing.T) {
	// A PI closed with ">" or "/>" used to be silently dropped from the
	// output; the contract is verbatim or error.
	for _, in := range []string{`<?xml version="1.0"/><svg/>`, `<?a><b/></a><svg/>`} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("Parse(%q) accepted a malformed processing instruction", in)
		}
	}
}
