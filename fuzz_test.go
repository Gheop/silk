package silk

import (
	"bytes"
	"testing"
)

// FuzzOptimize checks totality and the public guarantees on arbitrary bytes:
// no panic, and when optimization succeeds the result is stable under a
// second run (idempotence) and never larger than the input.
func FuzzOptimize(f *testing.F) {
	seeds := []string{
		`<svg><path d="M0 0L10 10z"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><g fill="red"><path d="M0 0h1v1H0z"/><path d="m5 5h1v1h-1z"/></g></svg>`,
		`<?xml version="1.0"?><!DOCTYPE svg><svg><!-- c --><metadata>m</metadata><g transform="translate(1,2) scale(3)"><path d="M.5.5a5 5 0 0110 10"/></g></svg>`,
		`<svg><style>.a{fill:url(#p)}</style><defs><linearGradient id="p"/></defs><rect class="a"/></svg>`,
		`<svg><g><g><g/></g></g><use href="#x"/><path id="x" d="M0 0 5 5"/></svg>`,
		`<svg><path d="M0 0C1 1 2 2 3 3S5 5 6 6Q7 7 8 8T9 9"/></svg>`,
		`<a b="&amp;&#65;"/>`,
		`<svg><path d="M1e2.5-.5.5"/></svg>`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	opts := DefaultOptions()
	f.Fuzz(func(t *testing.T, in []byte) {
		out, err := Optimize(in, opts)
		if err != nil {
			return
		}
		if len(out) > len(in) {
			t.Fatalf("grew: %d -> %d", len(in), len(out))
		}
		again, err := Optimize(out, opts)
		if err != nil {
			t.Fatalf("output does not re-optimize: %v\nout: %.200q", err, out)
		}
		if !bytes.Equal(again, out) {
			t.Fatalf("not idempotent:\n 1: %.200q\n 2: %.200q", out, again)
		}
	})
}
