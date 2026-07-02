# silk

`silk` shrinks SVG files by rewriting path geometry and document structure,
in pure Go — no cgo, no external runtime. It covers the parts of Node's
`svgo` that actually matter for size (`convertPathData`, `mergePaths`, and a
handful of safe structural passes) for services that cannot embed a Node
toolchain. The output renders identically to the input: any construct whose
optimization cannot be proven safe is emitted byte-for-byte unchanged.

## Install

```
go get github.com/Gheop/silk
```

A small CLI is included:

```
go run github.com/Gheop/silk/cmd/silk@latest -precision 3 input.svg > output.svg
```

## Usage

```go
out, err := silk.Optimize(svgBytes, silk.DefaultOptions())
if err != nil {
    // input was not parseable XML; fall back to your own minifier
}
```

### Options

```go
type Options struct {
    // Precision is the maximum number of decimal places kept for coordinates
    // and path data. 0 means exact (no rounding). Rounding is the single
    // biggest lever and the main fidelity risk, so it is opt-in and bounded.
    Precision int

    // TransformPrecision rounds transform translation components when > 0.
    // By default transforms are only rewritten losslessly: rounding a group
    // translation shifts whole subtrees coherently, which sub-pixel patterns
    // turn into visible moiré.
    TransformPrecision int

    // Multipass reruns the pass pipeline until the byte length stops
    // shrinking, bounded by MaxPasses (default 8 when Multipass is true).
    Multipass bool
    MaxPasses int
}
```

`DefaultOptions()` returns `{Precision: 3, Multipass: true}`.

The zero value of `Options` is safe and conservative: exact numbers, no
rounding, minimal passes.

## Guarantees

- **Visually lossless.** The result renders identically to the input within
  the configured precision tolerance, verified pixel-by-pixel over a corpus
  of 50 real-world files. Fidelity-sensitive spots automatically keep more
  precision than asked: tiny segments whose direction stroke joins amplify,
  near-degenerate arcs, almost-closed subpaths; segment removal stays off
  under filters, whose regions sample the geometry.
- **Deterministic.** Identical `(svg, opts)` yields byte-identical output.
- **Idempotent.** `Optimize(Optimize(x)) == Optimize(x)`, byte for byte. The
  pipeline runs to a byte fixed point; if none is reached the input is
  returned unchanged.
- **Total.** Never panics, never loops forever, bounded memory — including
  on malformed, hostile, or truncated input (continuously fuzzed). On
  unparseable input it returns `(nil, error)` so the caller can fall back.

## What it does

Path data (`d` attributes): shortest-form numbers, minimal separators,
absolute↔relative per command, shorthands (`H`/`V`/`S`/`T`, implicit
repeats), flat curves rewritten as lines (control points inside the
tolerance tube of the chord), collinear line runs folded, removal of no-op
segments and empty subpaths when provably invisible, precision rounding
with drift-free error tracking (every delta is taken against the emitted
point).

Structure: comment/metadata/editor-namespace removal (Inkscape, Illustrator,
Sketch, …), unreferenced definitions inside `<defs>`, insignificant
whitespace (between elements and inside tags), empty containers, group
collapsing, transform-list flattening, merging of adjacent paths with
identical attributes and provably disjoint geometry.

Styling: inline `style` becomes presentation attributes when no stylesheet
could outrank them, declarations set to their initial value drop (with
inheritance analysis), colors take their shortest spelling, and numeric
attributes (shape geometry, `points`, opacities, stroke metrics) round to
the configured precision.

A reference graph (ids targeted by `url(#…)`, `href`, `aria-*`, stylesheet
text) marks everything referenced as untouchable. A `<style>` element
disables structural element removal and merging entirely — selectors are not
resolved, so anything they might match is preserved.

Out of scope: sanitization (scripts, event handlers, external references are
not removed — run a sanitizer first), rasterization, SVG generation, and
animation.

## Benchmark

Corpus: 50 real-world SVGs (scans, line art, illustrations, icons), compared
against `svgo` (via `npx svgo`, default settings). Size is percent of input
after optimization, lower is better.

| File | Input | silk | svgo |
|---|---:|---:|---:|
| CrystalTreeofLife_SVG.svg | 592 KiB | **12.2 %** | 11.0 % |
| 2024-08-17…ReconstHisto-d.svg | 1.6 MiB | 57.4 % | 43.4 % |
| 2024-08-17…ReconstHisto-g.svg | 1.6 MiB | 56.7 % | 42.8 % |
| Coloriage-TDF-Citadelle.svg | 514 KiB | 52.0 % | 47.3 % |
| OSSMS-Vaivre.svg | 5.3 MiB | 57.6 % | 50.6 % |
| Feedback_Punkteabfrage.svg | 611 KiB | 33.2 % | 27.1 % |
| Lo-Fi_House_Vinyl_Cover.svg | 2.1 MiB | 34.2 % | 34.2 % |
| Fuehrung.svg | 333 KiB | **65.4 %** | fails to parse |
| **Whole corpus (50 files)** | 30.5 MiB | **72.0 %** | 64.6 % |
| **Median ratio** | | **71 %** | 58.8 % |

svgo's remaining ~12-point median edge comes from passes outside silk's
scope, several of which trade correctness for size: svgo drops embedded SVG
fonts that live `<text>` still references, renames every id (breaking
external sprite references), and rewrites presentation attributes wholesale.
silk only applies transforms whose safety it can prove.

Fidelity, measured with the bundled resvg pixel harness on the same corpus:
silk passes every file; svgo's default precision exceeds the same per-pixel
tolerance on several line-art files (its worst file leaves ~6× more
strongly-diverging pixels than silk's worst).

Speed: in-process, silk handles small icons in tens of
microseconds and the corpus's 1.5 MiB single-path scans in ~150-280 ms
(≈ 6-10 MiB/s end to end, dominated by candidate re-encoding). The `svgo`
subprocess needs 1.2-30 s per file on the same machine including Node
startup — two orders of magnitude slower for a service calling it per
image.

## Fidelity harness

Correctness is proven by rendering, not inspection. The test suite renders
original and optimized documents with [resvg] at 512 px and compares pixels:
at most 0.2 % of pixels may differ by more than 8/255 per channel, and at
most 0.02 % by more than 64/255. Any corpus file beyond that fails the
suite.

```
# resvg must be on PATH (tests skip cleanly without it)
go test ./...
```

The corpus location is set in `silk_test.go` (`corpusDir`); point it at any
directory of SVG files.

Fuzzing: `go test -fuzz=FuzzOptimize .` exercises the whole optimizer;
`go test -fuzz=FuzzParse ./internal/path/` exercises the path grammar.

[resvg]: https://github.com/linebender/resvg

## Changelog

### v0.2.0 — Styling passes, curve straightening, big speedups (2026-07-02)

- New: inline styles convert to presentation attributes, default-valued
  declarations drop, colors shorten, numeric attributes round.
- New: flat curves become lines and collinear line runs fold, within the
  same tolerance budget as coordinate rounding.
- New: unreferenced `<defs>` entries and editor blobs behind DTD entities
  (Illustrator `<i:pgf>`) are now found and removed.
- Whole-corpus output is now smaller than svgo's total (63.6 % vs 64.6 % of
  input); median gap narrowed to ~4.6 points with fidelity svgo does not
  match.
- 2-4× faster: allocation churn cut ~7×, losing encoding candidates are
  costed arithmetically instead of being formatted, merge decisions reuse
  cached geometry.

### v0.1.0 — Initial release (2026-07-02)

- Path-data optimizer: shortest encodings, drift-free rounding, automatic
  extra precision where rounding is visually amplified.
- Structural passes: cleanup, group collapsing, transform flattening,
  adjacent-path merging, all gated by a reference-safety graph.
- Pixel-fidelity test harness (resvg) and 50-file corpus gate.
- Guarantees: deterministic, idempotent, total; unparseable input reports an
  error instead of risking the document.
