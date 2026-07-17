# silk

[![ci](https://github.com/Gheop/silk/actions/workflows/ci.yml/badge.svg)](https://github.com/Gheop/silk/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/Gheop/silk.svg)](https://pkg.go.dev/github.com/Gheop/silk)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

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

Or as a container image (GitHub and GitLab registries):

```
docker run -i ghcr.io/gheop/silk < input.svg > output.svg
docker run -i registry.gitlab.com/gheop/silk < input.svg > output.svg
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
tolerance tube of the chord), collinear line runs folded, runs of cubics
tracing a common circle rewritten as arcs, elevated quadratics demoted to
`Q`/`T`, removal of no-op segments and empty subpaths when provably
invisible, precision rounding with drift-free error tracking (every delta
is taken against the emitted point). Basic shapes (`line`, sharp `rect`,
`polyline`, `polygon`) re-encode as paths when no stylesheet or script
could address them by type, which also lets adjacent identical ones merge.

Structure: comment/metadata/editor-namespace removal (Inkscape, Illustrator,
Sketch, …), unreferenced definitions inside `<defs>`, insignificant
whitespace (between elements and inside tags, including between structural
elements under `xml:space="preserve"` — it only governs text), empty
containers, group collapsing, transform-list flattening, merging of adjacent
paths with identical attributes and provably disjoint geometry, namespace
declarations that are redundant (re-declared in scope) or unused, and inert
attributes (`version`, zero viewport offsets, `xml:space` that provably
cannot change text rendering). Embedded font glyph outlines get the same
shortest-form re-encoding as visible paths.

Styling: inline `style` becomes presentation attributes when no stylesheet
could outrank them, declarations set to their initial value drop (with
inheritance analysis), colors take their shortest spelling, numeric
attributes (shape geometry, `points`, opacities, stroke metrics) round to
the configured precision, and `<style>` sheets lose their indentation (never
touched when they carry strings or escapes).

A reference graph (ids targeted by `url(#…)`, `href`, `aria-*`, stylesheet
text) marks everything referenced as untouchable. A `<style>` element
disables structural element removal and merging entirely — selectors are not
resolved, so anything they might match is preserved.

Out of scope: sanitization (scripts, event handlers, external references are
not removed — run a sanitizer first), rasterization, SVG generation, and
animation.

## Benchmark

Corpus: 100 real-world SVGs (scans, line art, illustrations, diagrams,
icons), compared against `svgo` (via `npx svgo`, default settings). Size is
percent of input after optimization, lower is better.

| File | Input | silk | svgo |
|---|---:|---:|---:|
| 2024-08-17…ReconstHisto-d.svg | 1.6 MiB | **31.2 %** | 43.4 % |
| OSSMS-Vaivre.svg | 5.3 MiB | **36.0 %** | 50.6 % |
| SFI.svg | 2.3 MiB | 56.4 % | **52.9 %** |
| Coloriage-TDF-Citadelle.svg | 514 KiB | **32.9 %** | 47.3 % |
| Ruler_illustration.svg | 52 KiB | **37.2 %** | 37.7 % |
| CrystalTreeofLife_SVG.svg | 592 KiB | 11.8 % | **11.0 %** |
| Jade_dragon.svg | 211 KiB | **33.2 %** | 33.6 % |
| Feedback_Punkteabfrage.svg | 611 KiB | 27.8 % | **27.1 %** |
| Lo-Fi_House_Vinyl_Cover.svg | 2.1 MiB | 34.2 % | 34.2 % |
| Le_Fritkot_BW.svg | 304 KiB | 64.7 % | **53.6 %** |
| Fuehrung.svg | 333 KiB | **55.0 %** | fails to parse |
| **Whole corpus (100 files)** | 42.3 MiB | **64.3 %** | 65.6 % |
| **Median ratio** | | 58.3 % | **57.0 %** |

Fidelity, measured with the bundled resvg pixel harness on the same corpus:
silk passes all 100 files. svgo fails 14 of them — one does not parse (DTD
entity limits), one loses its entire background (a dark textured infographic
comes back white, 73 % of pixels wrong), and twelve exceed the pixel
tolerance, mostly dashed and hairline line art.

### Where the remaining median gap comes from

svgo's per-file median edge (1.3 points) is concentrated in passes silk
rejects deliberately, plus one honest remainder:

- **id renaming and removal.** svgo renames every id (`#petal` → `#a`) and
  deletes unreferenced ones. On files that reference an id repeatedly this
  is worth 10+ points — and it breaks every external consumer of the file
  (`sprite.svg#icon`, CSS `url(file.svg#filter)`). silk treats ids as
  public API.
- **Deleting content it does not recognize.** svgo drops legacy or foreign
  elements outright (Inkscape `<flowRoot>` text, mesh gradient internals).
  When that content is invisible to the renderer the bytes are free; when
  it is not, you get the wiped background above. silk only removes what is
  provably inert.
- **Merging overlapping paths.** svgo merges same-styled paths whose
  geometry overlaps; with fill rules and opacity in play that reorders and
  double-paints. silk merges only provably disjoint geometry.
- **The honest remainder: curve rewriting depth.** svgo still converts more
  curve shapes than silk (ellipse-fitting arcs, aggressive shorthand use at
  the tolerance boundary). silk closed most of this in v0.4.0 — circular
  cubic runs become arcs, elevated quadratics demote — and keeps the rest
  conservative because that margin is exactly where svgo's twelve
  tolerance failures live (dash phase drift, flattened hairlines).

Speed, in-process: small icons in ~50 µs, the 1.5 MiB single-path scans in
~85 ms, the 5.3 MiB corpus outlier in ~400 ms. The `svgo` subprocess needs
0.7-16 s per file on the same machine including Node startup — 10-180×
slower for a service invoking it per image.

## Fidelity harness

Correctness is proven by rendering, not inspection. The test suite renders
original and optimized documents with [resvg] at 512 px and compares pixels:
at most 0.2 % of pixels may differ by more than 8/255 per channel, and at
most 0.02 % by more than 64/255; when not a single pixel exceeds 64/255,
up to 0.5 % may carry the smaller anti-aliasing shifts. Any corpus file
beyond that fails the suite.

```
# resvg must be on PATH (tests skip cleanly without it)
go test ./...
```

The corpus location is set in `silk_test.go` (`corpusDir`); point it at any
directory of SVG files.

Fuzzing: `go test -fuzz=FuzzOptimize .` exercises the whole optimizer;
`go test -fuzz=FuzzParse ./internal/path/` exercises the path grammar.

[resvg]: https://github.com/linebender/resvg

## Used by

silk is the SVG stage of [patu.dev](https://patu.dev), an asset compression
API: POST a raw file, get the optimized bytes back. The benchmark corpus
above is drawn from its real-world workload.

## Changelog

### v0.4.0 — Arc and quadratic conversion, shape-to-path, corpus doubled (2026-07-03)

- New: runs of cubic curves tracing a common circle become endpoint arcs
  (with the radius snapped to the round number the drawing meant), and
  cubics that are elevated quadratics demote to `Q`/`T` — the biggest
  remaining svgo levers, implemented under the pixel-fidelity gate.
- New: `line`, sharp-cornered `rect`, `polyline` and `polygon` re-encode as
  paths when no stylesheet or script could address them by type; adjacent
  identical shapes then merge (a 941-line ruler drops from 97 % to 37 %).
- New: fill-only paths skip the tiny-segment precision escalation that
  protects stroke joins — a 2.3 MiB fill-heavy trace drops 14 points.
- Fixed: attribute number lists were minified with path-style separator
  elision, which CSS reads as one malformed token (`stroke-dasharray`
  silently disabled, dashed lines rendering solid).
- Fixed: `stroke-dasharray` values are no longer rounded — a period error
  accumulates by the repeat count along the stroke and shifts far dashes.
- Fixed: arcs near the degenerate half-turn now reproduce the exact input
  chord; rounding either endpoint moved such arcs without bound (visible
  on Inkscape ellipses exported as two half-turn arcs).
- Fixed: the last point before a closepath no longer leaks raw float64
  text (up to 20 decimals) in rare configurations.
- The corpus doubled to 100 files (42.3 MiB): silk 64.3 % of input vs svgo
  65.6 %; per-file median 58.3 % vs 57.0 %. Fidelity: silk passes 100/100,
  svgo fails 14 (one parse failure, one wiped background, twelve over
  pixel tolerance).

### v0.3.0 — Namespace and xml:space cleanup, glyph outlines, close-vector encoding (2026-07-03)

- Fixed: the last point before a closepath was emitted with full float64
  precision when the closing vector was small (up to 20 decimals per
  number); it now takes the fewest decimals that still pin the closing
  direction. Line-art outputs shrink up to 5 %.
- New: whitespace between structural elements is removed under
  `xml:space="preserve"` too — the attribute only governs text content.
  The attribute itself drops when the text it covers renders identically
  either way, along with `version`, zero viewport offsets, redundant
  re-declared namespaces, and namespace prefixes nothing uses.
- New: embedded SVG font glyphs (`<glyph>`, `<missing-glyph>`) get the same
  shortest-form path re-encoding as visible paths.
- New: `<style>` sheet indentation collapses when provably safe (no strings,
  escapes, or markup in the sheet).
- Whole corpus drops from 63.6 % to 62.6 % of input; median from 64.3 % to
  60.7 %. Illustrator-generated files gain the most (Pictomago -23 pts,
  Fuehrung -10 pts, Feedback -5 pts).
- Slightly faster on big files (~10 % on the 1.5 MiB scans) — less
  whitespace and shorter numbers to carry through the pipeline.

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
- CI on GitHub Actions and GitLab, and a container image published to both
  registries.

### v0.1.0 — Initial release (2026-07-02)

- Path-data optimizer: shortest encodings, drift-free rounding, automatic
  extra precision where rounding is visually amplified.
- Structural passes: cleanup, group collapsing, transform flattening,
  adjacent-path merging, all gated by a reference-safety graph.
- Pixel-fidelity test harness (resvg) and 50-file corpus gate.
- Guarantees: deterministic, idempotent, total; unparseable input reports an
  error instead of risking the document.
