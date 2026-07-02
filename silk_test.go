package silk

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Gheop/silk/internal/fidelity"
)

const corpusDir = "/home/sib/src/benchmarkpatu/datasets"

func corpusSVGs(t *testing.T) []string {
	t.Helper()
	if _, err := os.Stat(corpusDir); err != nil {
		t.Skipf("corpus not available: %v", err)
	}
	var files []string
	filepath.WalkDir(corpusDir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(path) == ".svg" {
			files = append(files, path)
		}
		return nil
	})
	if len(files) == 0 {
		t.Skip("corpus contains no SVG files")
	}
	return files
}

// The corpus gate: every corpus file must optimize deterministically,
// idempotently, never grow, and render pixel-identically.
func TestCorpus(t *testing.T) {
	opts := Options{Precision: 3}
	for _, f := range corpusSVGs(t) {
		f := f
		t.Run(filepath.Base(f), func(t *testing.T) {
			t.Parallel()
			in, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			out, err := Optimize(in, opts)
			if err != nil {
				t.Fatalf("Optimize: %v", err)
			}
			if len(out) > len(in) {
				t.Errorf("grew: %d -> %d bytes", len(in), len(out))
			}
			again, err := Optimize(in, opts)
			if err != nil || !bytes.Equal(out, again) {
				t.Error("not deterministic")
			}
			twice, err := Optimize(out, opts)
			if err != nil {
				t.Fatalf("re-optimize: %v", err)
			}
			if !bytes.Equal(out, twice) {
				i := 0
				for i < len(out) && i < len(twice) && out[i] == twice[i] {
					i++
				}
				lo := max(0, i-30)
				t.Errorf("not idempotent at byte %d: %q vs %q",
					i, out[lo:min(len(out), i+30)], twice[lo:min(len(twice), i+30)])
			}
			fidelity.Compare(t, filepath.Base(f), in, out)
		})
	}
}
