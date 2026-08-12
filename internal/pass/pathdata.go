// Package pass hosts the document-level optimization passes. Each pass is an
// isolated function over the dom tree; anything it cannot prove safe it
// leaves byte-untouched.
package pass

import (
	"math"
	"runtime"
	"strings"
	"sync"

	"github.com/Gheop/silk/internal/dom"
	"github.com/Gheop/silk/internal/path"
)

// PathCache memoizes optimized path data across passes and pipeline
// iterations: the global fixed-point loop re-optimizes mostly unchanged
// documents, and the merge pass needs the same predictions the path pass
// computes. The mutex only matters during the parallel prewarm; the passes
// themselves run single-threaded.
type PathCache struct {
	mu sync.Mutex
	m  map[pathCacheKey]pathCacheVal
}

type pathCacheKey struct {
	d         string
	prec      int
	noops     bool
	collinear bool
}

type pathCacheVal struct {
	out   string
	ok    bool
	box   bbox // control bbox of the emitted geometry
	boxOK bool
}

func NewPathCache() *PathCache {
	return &PathCache{m: map[pathCacheKey]pathCacheVal{}}
}

// optimize returns the emitted encoding for d under the given options: the
// local byte fixed point of the path optimizer, so re-optimizing the result
// changes nothing. ok is false when the path data does not parse. Both the
// input and the fixed point are cached, which makes the global pipeline
// iterations nearly free.
func (c *PathCache) optimize(d string, prec int, noops, collinear bool) (string, bool) {
	key := pathCacheKey{d, prec, noops, collinear}
	c.mu.Lock()
	v, hit := c.m[key]
	c.mu.Unlock()
	if hit {
		return v.out, v.ok
	}
	cs, err := path.Parse([]byte(d))
	if err != nil {
		c.store(key, pathCacheVal{})
		return "", false
	}
	cur := d
	for range 5 {
		out, emitted := path.OptimizeEmitted(cs, path.Options{Precision: prec, RemoveNoops: noops, MergeCollinear: collinear})
		if s := string(out); s == cur {
			// The emitted command list is in hand: measuring the bbox here
			// costs one walk, and spares the merge pass any re-parsing.
			box, boxOK := controlBBox(emitted)
			v := pathCacheVal{s, true, box, boxOK}
			c.store(key, v)
			c.store(pathCacheKey{s, prec, noops, collinear}, v)
			return s, true
		} else {
			cur = s
		}
		cs = emitted
	}
	// No fixed point within the bound: leaving the data untouched is the
	// only stable answer.
	box, boxOK := controlBBox(cs)
	c.store(key, pathCacheVal{d, true, box, boxOK})
	return d, true
}

// emittedBBox returns the control bbox of the geometry the path pass emits
// for d, computing (and caching) it on demand.
func (c *PathCache) emittedBBox(d string, prec int, noops, collinear bool) (bbox, bool) {
	c.optimize(d, prec, noops, collinear)
	c.mu.Lock()
	v := c.m[pathCacheKey{d, prec, noops, collinear}]
	c.mu.Unlock()
	return v.box, v.ok && v.boxOK
}

func (c *PathCache) store(key pathCacheKey, v pathCacheVal) {
	c.mu.Lock()
	c.m[key] = v
	c.mu.Unlock()
}

// PrewarmPaths fills the cache for every path in the document concurrently.
// Results are deterministic — each entry is a pure function of its key — so
// only wall-clock time changes.
func PrewarmPaths(doc *dom.Node, prec int, cache *PathCache) {
	docSafe := noopSafeDoc(doc)
	type job struct {
		d         string
		prec      int
		noops     bool
		collinear bool
	}
	var jobs []job
	seen := map[pathCacheKey]bool{}
	doc.Walk(func(n *dom.Node) bool {
		if n.Kind != dom.KindElement || !pathDataElements[localName(n.Name)] {
			return true
		}
		d, ok := n.AttrValue("d")
		if !ok {
			return true
		}
		p, noops, collinear := pathOptions(n, prec, docSafe)
		key := pathCacheKey{d, p, noops, collinear}
		if !seen[key] {
			seen[key] = true
			jobs = append(jobs, job{d, p, noops, collinear})
		}
		return true
	})
	if len(jobs) < 2 {
		for _, j := range jobs {
			cache.optimize(j.d, j.prec, j.noops, j.collinear)
		}
		return
	}
	workers := min(runtime.GOMAXPROCS(0), len(jobs))
	var wg sync.WaitGroup
	next := make(chan job, len(jobs))
	for _, j := range jobs {
		next <- j
	}
	close(next)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range next {
				cache.optimize(j.d, j.prec, j.noops, j.collinear)
			}
		}()
	}
	wg.Wait()
}

// pathDataElements carry path data in a d attribute. Font glyphs draw in a
// context (text stroke, unsupported renderers) the document doesn't show, so
// they only ever get the conservative re-encoding, never segment surgery.
var pathDataElements = map[string]bool{
	"path": true, "glyph": true, "missing-glyph": true,
}

// pathOptions resolves the effective options for one path element.
func pathOptions(n *dom.Node, prec int, docSafe bool) (p int, noops, collinear bool) {
	if localName(n.Name) != "path" {
		return prec, false, false
	}
	if prec >= 0 {
		bump, ok := scaleBump(n)
		if !ok {
			return -1, false, false // unbounded amplification: keep exact
		}
		prec = min(prec+bump, 8)
	}
	if underFilter(n) {
		// A filter's primitives sample relative to the geometry, so segment
		// removal and vertex merging (which can change the tight bbox) stay
		// off; plain coordinate rounding measures within tolerance even
		// through feTurbulence on the corpus.
		return prec, false, false
	}
	if !markerSafeElement(n) {
		// An orient="auto" marker takes its rotation from vertex tangents,
		// whose length can be smaller than the rounding residual of the
		// shared vertex: no finite precision bounds the rotation, so paths
		// that concretely carry markers keep exact coordinates.
		return -1, false, false
	}
	if !dashSafeElement(n) && prec >= 0 {
		// A dash pattern integrates the whole path length: per-segment
		// length errors accumulate along the stroke by the segment count.
		// Two extra decimals keep the accumulated phase error below a
		// fraction of a period on paths thousands of segments long.
		return min(prec+2, 10), false, false
	}
	return prec, docSafe && noopSafeElement(n), docSafe && markerSafeElement(n)
}

// scaleBump returns the extra decimals the element's cumulative transform
// scale demands: a subtree scaled ×200 turns a half-thousandth rounding into
// a 0.1-unit displacement in the viewport. The bound is the product of the
// max column norms of every transform in scope; an unparseable transform
// reports ok=false (no bound at all).
func scaleBump(n *dom.Node) (int, bool) {
	s := 1.0
	for e := n; e != nil && e.Kind == dom.KindElement; e = e.Parent {
		if !e.HasAttr("transform") {
			continue
		}
		v, ok := e.AttrValue("transform")
		if !ok {
			return 0, false
		}
		m, err := parseTransformList(v)
		if err != nil {
			return 0, false
		}
		s *= max(math.Hypot(m.a, m.b), math.Hypot(m.c, m.d))
	}
	// Scales up to ×4 keep the displacement of a half-thousandth rounding
	// under two thousandths of a unit — below anything the pixel gate can
	// see. Beyond that, one decimal per decade.
	s /= 4
	bump := 0
	for s > 1 && bump < 8 {
		s /= 10
		bump++
	}
	return bump, true
}

// dashSafeElement reports whether the element provably carries no dash
// pattern, looking at its own and its ancestors' attributes and inline
// styles (stroke-dasharray inherits).
func dashSafeElement(n *dom.Node) bool {
	for e := n; e != nil && e.Kind == dom.KindElement; e = e.Parent {
		for i := range e.Attrs {
			a := &e.Attrs[i]
			switch a.Name {
			case "stroke-dasharray":
				if v, ok := a.Value(); !ok || strings.TrimSpace(strings.ToLower(v)) != "none" {
					return false
				}
			case "style":
				if v, ok := a.Value(); !ok || strings.Contains(v, "dasharray") {
					return false
				}
			}
		}
	}
	return true
}

// markerSafeElement reports whether the element provably carries no markers:
// merging collinear vertices only ever shows through markers.
func markerSafeElement(n *dom.Node) bool {
	for e := n; e != nil && e.Kind == dom.KindElement; e = e.Parent {
		for i := range e.Attrs {
			a := &e.Attrs[i]
			switch a.Name {
			case "marker", "marker-start", "marker-mid", "marker-end":
				return false
			case "style":
				if v, ok := a.Value(); !ok || strings.Contains(v, "marker") {
					return false
				}
			}
		}
	}
	return true
}

// OptimizePaths rewrites every d attribute whose optimized encoding is
// strictly shorter. Unparseable path data is left as found.
func OptimizePaths(doc *dom.Node, prec int, cache *PathCache) {
	docSafe := noopSafeDoc(doc)
	doc.Walk(func(n *dom.Node) bool {
		if n.Kind != dom.KindElement || !pathDataElements[localName(n.Name)] {
			return true
		}
		d, ok := n.AttrValue("d")
		if !ok {
			return true
		}
		p, noops, collinear := pathOptions(n, prec, docSafe)
		out, ok := cache.optimize(d, p, noops, collinear)
		// An empty result is only valid when the input path was itself
		// empty of any drawing.
		if ok && len(out) > 0 && len(out) < len(d) {
			n.SetAttr("d", out)
		}
		return true
	})
}

func localName(name string) string {
	if i := strings.IndexByte(name, ':'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// underSwitch reports whether n is a direct child of a <switch> element.
// <switch> renders only its first eligible child, so the identity, count and
// order of its children are semantic: unwrapping, removing or merging them
// changes which subtree renders.
func underSwitch(n *dom.Node) bool {
	return n.Parent != nil && n.Parent.Kind == dom.KindElement &&
		localName(n.Parent.Name) == "switch"
}

// noopSafeDoc reports whether zero-length segments can be dropped anywhere in
// the document. A stylesheet could set stroke or markers through selectors we
// do not resolve, and <use> can re-render a path under different inherited
// properties, so either disables removal wholesale.
func noopSafeDoc(doc *dom.Node) bool {
	safe := true
	doc.Walk(func(n *dom.Node) bool {
		switch {
		case n.Kind == dom.KindElement && localName(n.Name) == "style",
			n.Kind == dom.KindElement && localName(n.Name) == "use",
			n.Kind == dom.KindProcInst && n.Name == "xml-stylesheet":
			safe = false
			return false
		}
		return safe
	})
	return safe
}

// underFilter reports whether a filter applies to the element or any of its
// ancestors.
func underFilter(n *dom.Node) bool {
	for e := n; e != nil && e.Kind == dom.KindElement; e = e.Parent {
		if e.HasAttr("filter") {
			return true
		}
		if e.HasAttr("style") {
			if v, ok := e.AttrValue("style"); !ok || strings.Contains(v, "filter") {
				return true
			}
		}
	}
	return false
}

// noopSafeElement reports whether the element is provably unstroked and
// unmarkered: zero-length segments render nothing only then.
func noopSafeElement(n *dom.Node) bool {
	strokeResolved := false
	for e := n; e != nil && e.Kind == dom.KindElement; e = e.Parent {
		for i := range e.Attrs {
			a := &e.Attrs[i]
			v, ok := a.Value()
			switch a.Name {
			case "stroke":
				if !strokeResolved {
					if !ok || strings.TrimSpace(v) != "none" {
						return false
					}
					strokeResolved = true
				}
			case "style":
				// Rough but conservative: any mention of stroke or marker in
				// inline CSS blocks removal.
				if !ok || strings.Contains(v, "stroke") || strings.Contains(v, "marker") {
					return false
				}
			case "marker", "marker-start", "marker-mid", "marker-end":
				return false
			}
		}
	}
	return true
}
