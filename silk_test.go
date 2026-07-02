package silk

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gheop/silk/internal/fidelity"
)

var corpusDir = func() string {
	if d := os.Getenv("SILK_CORPUS"); d != "" {
		return d
	}
	return "testdata/corpus"
}()

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

func TestTotality(t *testing.T) {
	hostile := []string{
		"",
		"   ",
		"not xml at all",
		"<svg",
		"<svg>",
		"<svg></svg",
		"<svg><path d=\"M0 0",
		"\x00\x01\x02",
		"<svg xmlns=\"a\"><path d=\"M0 0l" + strings.Repeat("1 ", 100000) + "\"/></svg>",
		"<svg>" + strings.Repeat("<g>", 20000),
		"<a></b>",
	}
	for _, in := range hostile {
		out, err := Optimize([]byte(in), DefaultOptions())
		if err == nil {
			if _, err2 := Optimize(out, DefaultOptions()); err2 != nil {
				t.Errorf("%.40q: output does not re-optimize: %v", in, err2)
			}
		}
	}
	// Deep but balanced nesting must not crash either.
	deep := strings.Repeat("<g>", 5000) + `<path d="M0 0h1"/>` + strings.Repeat("</g>", 5000)
	if _, err := Optimize([]byte(deep), DefaultOptions()); err != nil {
		t.Errorf("deep nesting: %v", err)
	}
}

func TestOptimizeDoesNotMutateInput(t *testing.T) {
	in := []byte(`<svg><path d="M0 0 L10 10"/></svg>`)
	orig := append([]byte(nil), in...)
	if _, err := Optimize(in, DefaultOptions()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(in, orig) {
		t.Error("input buffer was mutated")
	}
}

func TestDefaultOptions(t *testing.T) {
	o := DefaultOptions()
	if o.Precision != 3 || !o.Multipass {
		t.Errorf("unexpected defaults: %+v", o)
	}
}

func TestZeroValueOptionsAreExact(t *testing.T) {
	in := []byte(`<svg><path d="M0.123456789 0 L10 10"/></svg>`)
	out, err := Optimize(in, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte(".123456789")) {
		t.Errorf("zero options must not round: %s", out)
	}
}

func TestUnparseableReturnsError(t *testing.T) {
	if _, err := Optimize([]byte(`<a></b>`), DefaultOptions()); err == nil {
		t.Error("expected error for mismatched tags")
	}
	if _, err := Optimize(nil, DefaultOptions()); err == nil {
		t.Error("expected error for empty input")
	}
}

// BenchmarkOptimizeLarge measures throughput on the largest corpus files;
// the target is well under 100 ms for the ~1.6 MB inputs.
func BenchmarkOptimizeLarge(b *testing.B) {
	f := corpusDir + "/formats/2024-08-17_12-11-58-d_ReconstHisto-Sentheim.svg"
	in, err := os.ReadFile(f)
	if err != nil {
		b.Skip(err)
	}
	opts := DefaultOptions()
	b.SetBytes(int64(len(in)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := Optimize(in, opts); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOptimizeSmall(b *testing.B) {
	in := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-2 15l-5-5 1.41-1.41L10 14.17l7.59-7.59L19 8l-9 9z"/></svg>`)
	opts := DefaultOptions()
	b.ResetTimer()
	for b.Loop() {
		if _, err := Optimize(in, opts); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOptimizeManyPaths measures the many-small-paths shape (merge-heavy).
func BenchmarkOptimizeManyPaths(b *testing.B) {
	f := corpusDir + "/formats/E8out96g16.svg"
	in, err := os.ReadFile(f)
	if err != nil {
		b.Skip(err)
	}
	opts := DefaultOptions()
	b.SetBytes(int64(len(in)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := Optimize(in, opts); err != nil {
			b.Fatal(err)
		}
	}
}
