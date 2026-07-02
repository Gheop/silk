package path

import (
	"bytes"
	"testing"
)

// FuzzParse checks the parser is total and that serialization is a fixed
// point: whatever parses must survive serialize -> parse unchanged.
func FuzzParse(f *testing.F) {
	seeds := []string{
		"M0 0L10 10z",
		"M0 0 10 10 20 20",
		"m.5.5l-.25.75",
		"M0 0a5 5 0 0110 10a5,5,0,0,1,10,10",
		"M1e2.5-.5.5",
		"M0 0C1 2 3 4 5 6S7 8 9 10Q1 2 3 4T5 6H7V8",
		"M10-20l+5+5",
		"M0 0h1e+2v1E-2",
		"M 0 , 0 Z m 5 5 z",
		"M0 0a5 5 0 2 1 1 1",
		"M--5 0",
		"M0 0l5. 5",
		"",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, d []byte) {
		cs, err := Parse(d)
		if err != nil {
			return
		}
		out := Serialize(nil, cs, -1)
		back, err := Parse(out)
		if err != nil {
			t.Fatalf("serialized form does not re-parse: %q: %v", out, err)
		}
		if !equalCmds(cs, back) {
			t.Fatalf("round-trip changed commands: %q -> %v -> %q -> %v", d, cs, out, back)
		}
		out2 := Serialize(nil, back, -1)
		if !bytes.Equal(out, out2) {
			t.Fatalf("serialization not a fixed point: %q vs %q", out, out2)
		}
	})
}
