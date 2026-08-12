// Package silk shrinks SVG documents by rewriting path geometry and
// structure, in pure Go. The output renders identically to the input: any
// construct that cannot be optimized provably safely is emitted unchanged.
package silk

import (
	"bytes"
	"math"
	"strconv"
	"strings"

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
	// Idempotence requires a byte fixed point: some decisions (shorthand
	// eligibility, merge safety) are threshold-sensitive and could otherwise
	// flip when re-run on their own output. The pipeline reruns until the
	// bytes stabilize; Multipass only raises how many shrinking iterations
	// are allowed before that point.
	bound := 4
	if opts.Multipass {
		bound = 8
		if opts.MaxPasses > 0 {
			bound = opts.MaxPasses
		}
	}
	cache := pass.NewPathCache()
	out, err := optimizeOnce(svg, opts, cache)
	if err != nil {
		return nil, err
	}
	for range bound {
		next, err := optimizeOnce(out, opts, cache)
		if err != nil {
			break
		}
		if bytes.Equal(next, out) {
			if len(out) >= len(svg) {
				return clone(svg), nil
			}
			return out, nil
		}
		out = next
	}
	// No fixed point within the bound: the input is the only answer that is
	// both safe and idempotent.
	return clone(svg), nil
}

func optimizeOnce(svg []byte, opts Options, cache *pass.PathCache) ([]byte, error) {
	doc, err := dom.Parse(svg)
	if err != nil {
		return nil, err
	}
	refs := pass.Analyze(doc)
	prec := pathPrecision(opts, doc)
	pass.Cleanup(doc, refs)
	pass.OptimizePresentation(doc, refs, prec)
	pass.CollapseGroups(doc, refs)
	pass.ConvertTransforms(doc, transformPrecision(opts))
	pass.ConvertShapes(doc, refs)
	pass.PrewarmPaths(doc, prec, cache)
	pass.MergePaths(doc, refs, prec, cache)
	pass.OptimizePaths(doc, prec, cache)
	return dom.Serialize(doc), nil
}

// pathPrecision maps the public contract (0 = exact) onto the internal one
// (negative = exact). Documents whose root viewBox spans only a few units
// (drawings exported in physical units: a CorelDRAW trace lives in 2×3
// inches) get extra decimals: the requested count is meant for the usual
// hundreds-of-units canvas, and on a two-unit canvas the same tolerance is
// a visible fraction of the whole image.
func pathPrecision(opts Options, doc *dom.Node) int {
	if opts.Precision <= 0 {
		return -1
	}
	p := opts.Precision
	if s := docScale(doc); s > 0 && s < 10 {
		if need := int(math.Ceil(5.4 - math.Log10(s))); need > p {
			p = min(need, 8)
		}
	}
	return p
}

// docScale returns the larger dimension of the root svg viewBox, falling
// back to unitless (or px) width/height; 0 when unknown.
func docScale(doc *dom.Node) float64 {
	var root *dom.Node
	for _, c := range doc.Children {
		if c.Kind == dom.KindElement {
			name := c.Name
			if i := strings.IndexByte(name, ':'); i >= 0 {
				name = name[i+1:]
			}
			if name == "svg" {
				root = c
			}
			break
		}
	}
	if root == nil {
		return 0
	}
	if vb, ok := root.AttrValue("viewBox"); ok {
		f := strings.FieldsFunc(vb, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' })
		if len(f) == 4 {
			w, err1 := strconv.ParseFloat(f[2], 64)
			h, err2 := strconv.ParseFloat(f[3], 64)
			if err1 == nil && err2 == nil {
				return max(math.Abs(w), math.Abs(h))
			}
		}
		return 0
	}
	dim := 0.0
	for _, name := range [...]string{"width", "height"} {
		v, ok := root.AttrValue(name)
		if !ok {
			continue
		}
		v = strings.TrimSuffix(strings.TrimSpace(v), "px")
		if x, err := strconv.ParseFloat(v, 64); err == nil {
			dim = max(dim, math.Abs(x))
		}
	}
	return dim
}

// transformPrecision only ever rounds transforms when explicitly asked to.
// Rounding a group translation shifts entire subtrees coherently, which
// sub-pixel patterns (fine hatching) turn into visible moiré; unlike path
// coordinates there is no content-local way to bound that effect.
func transformPrecision(opts Options) int {
	if opts.TransformPrecision > 0 {
		return opts.TransformPrecision
	}
	return -1
}

func clone(b []byte) []byte {
	return append([]byte(nil), b...)
}
