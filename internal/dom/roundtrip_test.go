package dom

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// corpusDir points at the benchmark corpus used throughout development. The
// test is skipped when it is absent so the suite still runs elsewhere.
const corpusDir = "/home/sib/src/benchmarkpatu/datasets"

func corpusSVGs(t *testing.T) []string {
	t.Helper()
	if _, err := os.Stat(corpusDir); err != nil {
		t.Skipf("corpus not available: %v", err)
	}
	var files []string
	err := filepath.WalkDir(corpusDir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(path) == ".svg" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("corpus contains no SVG files")
	}
	return files
}

func TestCorpusRoundTripVerbatim(t *testing.T) {
	for _, f := range corpusSVGs(t) {
		in, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := Parse(in)
		if err != nil {
			t.Errorf("%s: parse: %v", filepath.Base(f), err)
			continue
		}
		out := Serialize(doc)
		if !bytes.Equal(out, in) {
			i := 0
			for i < len(in) && i < len(out) && in[i] == out[i] {
				i++
			}
			lo, hi := max(0, i-40), min(len(in), i+40)
			t.Errorf("%s: round-trip diverges at byte %d\n in: %q\nout: %q",
				filepath.Base(f), i, in[lo:hi], out[lo:min(len(out), i+40)])
		}
	}
}
