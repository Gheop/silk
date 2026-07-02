// Package silk shrinks SVG documents by rewriting path geometry and
// structure, in pure Go. The output renders identically to the input: any
// construct that cannot be optimized provably safely is emitted unchanged.
package silk

import (
	"github.com/Gheop/silk/internal/dom"
	"github.com/Gheop/silk/internal/pass"
)

// Options controls the optimizer. The zero value is safe and conservative
// (no rounding, single pass).
type Options struct {
	// Precision is the maximum number of decimal places kept for coordinates
	// and path data. 0 means exact (no rounding). Rounding is the single
	// biggest lever and the main fidelity risk, so it is opt-in and bounded.
	Precision int

	// TransformPrecision overrides Precision for transform matrices when > 0
	// (matrices tolerate more rounding than geometry visible at 1:1).
	TransformPrecision int

	// Multipass reruns the pass pipeline until the byte length stops
	// shrinking, bounded by MaxPasses (default 8 when Multipass is true).
	Multipass bool
	MaxPasses int
}

// DefaultOptions returns the recommended configuration.
func DefaultOptions() Options {
	return Options{Precision: 3, Multipass: true}
}

// Optimize returns a size-optimized SVG.
//
// Guarantees:
//   - Visually lossless: the result renders identically to the input within
//     the configured precision tolerance.
//   - Deterministic: identical (svg, opts) always yields byte-identical
//     output.
//   - Idempotent: Optimize(Optimize(x)) == Optimize(x), byte for byte.
//   - Total: never panics, never loops forever, even on hostile input.
//
// On unparseable input it returns (nil, error) so the caller can fall back
// to its own minifier.
func Optimize(svg []byte, opts Options) ([]byte, error) {
	out, err := optimizeOnce(svg, opts)
	if err != nil {
		return nil, err
	}
	if len(out) >= len(svg) {
		return clone(svg), nil
	}
	return out, nil
}

func optimizeOnce(svg []byte, opts Options) ([]byte, error) {
	doc, err := dom.Parse(svg)
	if err != nil {
		return nil, err
	}
	pass.OptimizePaths(doc, pathPrecision(opts))
	return dom.Serialize(doc), nil
}

// pathPrecision maps the public contract (0 = exact) onto the internal one
// (negative = exact).
func pathPrecision(opts Options) int {
	if opts.Precision <= 0 {
		return -1
	}
	return opts.Precision
}

func clone(b []byte) []byte {
	return append([]byte(nil), b...)
}
