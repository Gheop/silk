package pass

import (
	"strconv"
	"strings"

	"github.com/Gheop/silk/internal/dom"
	"github.com/Gheop/silk/internal/path"
)

// presentationProps are CSS properties that exist as SVG 1.1 presentation
// attributes with identical value syntax. Only these may move from an
// inline style onto the element.
var presentationProps = map[string]bool{
	"fill": true, "fill-opacity": true, "fill-rule": true,
	"stroke": true, "stroke-width": true, "stroke-linecap": true,
	"stroke-linejoin": true, "stroke-miterlimit": true,
	"stroke-dasharray": true, "stroke-dashoffset": true, "stroke-opacity": true,
	"opacity": true, "color": true, "display": true, "visibility": true,
	"filter": true, "clip-path": true, "clip-rule": true, "mask": true,
	"marker-start": true, "marker-mid": true, "marker-end": true,
	"stop-color": true, "stop-opacity": true,
	"flood-color": true, "flood-opacity": true, "lighting-color": true,
	"font-family": true, "font-size": true, "font-style": true,
	"font-variant": true, "font-weight": true, "font-stretch": true,
	"letter-spacing": true, "word-spacing": true, "text-anchor": true,
	"text-decoration": true, "dominant-baseline": true, "baseline-shift": true,
	"direction": true, "writing-mode": true,
	"color-interpolation": true, "color-interpolation-filters": true,
	"shape-rendering": true, "text-rendering": true, "image-rendering": true,
	"color-rendering": true, "paint-order": true, "vector-effect": true,
}

// colorProps take color values.
var colorProps = map[string]bool{
	"fill": true, "stroke": true, "color": true, "stop-color": true,
	"flood-color": true, "lighting-color": true,
}

// defaultValues maps a property to its SVG initial value(s), normalized
// lowercase. Dropping a declaration set to its initial value changes
// nothing — unless the property inherits and an ancestor overrides it, or
// a stylesheet/use could (callers check).
var defaultValues = map[string][]string{
	"fill":              {"black", "#000", "#000000"},
	"fill-opacity":      {"1"},
	"fill-rule":         {"nonzero"},
	"stroke":            {"none"},
	"stroke-width":      {"1"},
	"stroke-linecap":    {"butt"},
	"stroke-linejoin":   {"miter"},
	"stroke-miterlimit": {"4"},
	"stroke-dasharray":  {"none"},
	"stroke-dashoffset": {"0"},
	"stroke-opacity":    {"1"},
	"opacity":           {"1"},
	"stop-opacity":      {"1"},
	"flood-opacity":     {"1"},
	"clip-rule":         {"nonzero"},
	"visibility":        {"visible"},
	"display":           {"inline"},
	"letter-spacing":    {"normal"},
	"word-spacing":      {"normal"},
}

// inheritedProps inherit in SVG; their defaults are only removable when no
// ancestor sets them and nothing (stylesheet, use) could re-parent the
// element under different values.
var inheritedProps = map[string]bool{
	"fill": true, "fill-opacity": true, "fill-rule": true,
	"stroke": true, "stroke-width": true, "stroke-linecap": true,
	"stroke-linejoin": true, "stroke-miterlimit": true,
	"stroke-dasharray": true, "stroke-dashoffset": true, "stroke-opacity": true,
	"color": true, "clip-rule": true, "visibility": true,
	"letter-spacing": true, "word-spacing": true,
}

// geoAttrs lists per-element geometric attributes that tolerate the same
// rounding as path coordinates: absolute values, error bounded by the
// precision tolerance, no drift. The root <svg> (document size), viewBox,
// and filter regions are deliberately absent.
var geoAttrs = map[string][]string{
	"rect":           {"x", "y", "width", "height", "rx", "ry"},
	"circle":         {"cx", "cy", "r"},
	"ellipse":        {"cx", "cy", "rx", "ry"},
	"line":           {"x1", "y1", "x2", "y2"},
	"use":            {"x", "y", "width", "height"},
	"image":          {"x", "y", "width", "height"},
	"linearGradient": {"x1", "y1", "x2", "y2"},
	"radialGradient": {"cx", "cy", "r", "fx", "fy", "fr"},
	"pattern":        {"x", "y", "width", "height"},
	"stop":           {"offset"},
	"text":           {"x", "y", "dx", "dy", "rotate"},
	"tspan":          {"x", "y", "dx", "dy", "rotate"},
	"polygon":        {"points"},
	"polyline":       {"points"},
}

// numericRoundProps are presentation properties whose values are plain
// numbers (or number lists) safe to round and minify.
var numericRoundProps = map[string]bool{
	"stroke-width": true, "stroke-dashoffset": true, "stroke-miterlimit": true,
	"stroke-dasharray": true, "opacity": true, "fill-opacity": true,
	"stroke-opacity": true, "stop-opacity": true, "flood-opacity": true,
}

// OptimizePresentation rewrites styling to its shortest equivalent form:
// inline styles become presentation attributes (shorter, and only when no
// stylesheet could outrank them), initial-value declarations drop, colors
// take their shortest spelling, and numeric attributes round to the
// configured precision.
func OptimizePresentation(doc *dom.Node, refs *Refs, prec int) {
	doc.Walk(func(n *dom.Node) bool {
		if n.Kind != dom.KindElement {
			return true
		}
		optimizeStyleAttr(n, refs, prec)
		if localName(n.Name) == "style" {
			minifyStylesheetText(n)
		}
		if names, ok := geoAttrs[localName(n.Name)]; ok {
			for _, name := range names {
				roundAttr(n, name, prec)
			}
		}
		for name := range numericRoundProps {
			roundAttr(n, name, prec)
		}
		optimizeColorAttrs(n, refs)
		return true
	})
}

// roundAttr minifies a numeric (or number-list) attribute value, keeping the
// result only when strictly shorter.
func roundAttr(n *dom.Node, name string, prec int) {
	if !n.HasAttr(name) {
		return
	}
	v, ok := n.AttrValue(name)
	if !ok {
		return
	}
	if s, ok := minifyNumbers(v, prec); ok && len(s) < len(v) {
		n.SetAttr(name, s)
	}
}

// minifyNumbers re-emits a comma/space separated number list (or a single
// number, tolerating a px suffix) in minimal form. Anything else — percent,
// other units, keywords — reports false.
func minifyNumbers(v string, prec int) (string, bool) {
	s := strings.TrimSpace(v)
	if s == "" {
		return "", false
	}
	if strings.HasSuffix(s, "px") && !strings.ContainsAny(s, " ,\t\n") {
		s = s[:len(s)-2]
	}
	var vals []float64
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n' || r == '\r'
	}) {
		x, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return "", false
		}
		vals = append(vals, x)
	}
	if len(vals) == 0 {
		return "", false
	}
	return string(path.AppendNumberList(nil, vals, prec)), true
}

// minifyStylesheetText collapses whitespace runs in a <style> element's text
// to single spaces: CSS treats a run as one separator everywhere but inside
// strings, so the pass backs off from sheets carrying quotes, escapes, or
// leftover markup (CDATA wrappers).
func minifyStylesheetText(n *dom.Node) {
	for _, c := range n.Children {
		if c.Kind != dom.KindText {
			continue
		}
		raw := string(c.Raw())
		if strings.ContainsAny(raw, "\"'\\<") {
			continue
		}
		out := strings.Join(strings.Fields(raw), " ")
		if len(out) < len(raw) {
			c.SetText(out)
		}
	}
}

func optimizeStyleAttr(n *dom.Node, refs *Refs, prec int) {
	if !n.HasAttr("style") {
		return
	}
	v, ok := n.AttrValue("style")
	if !ok {
		return
	}
	decls, ok := parseDecls(v)
	if !ok {
		return
	}
	var kept []decl
	for _, d := range decls {
		if strings.Contains(d.val, "!") {
			kept = append(kept, d) // !important: priority games, hands off
			continue
		}
		if colorProps[d.prop] {
			d.val = shortestColor(d.val)
		}
		if numericRoundProps[d.prop] {
			if s, ok := minifyNumbers(d.val, prec); ok && len(s) <= len(d.val) {
				d.val = s
			}
		}
		if isDroppableDefault(n, refs, d.prop, strings.ToLower(d.val)) {
			continue
		}
		// Inline style outranks stylesheet rules; a presentation attribute
		// does not. Without a stylesheet they are equivalent, and the
		// attribute form is shorter.
		if !refs.HasStylesheet && presentationProps[d.prop] {
			n.SetAttr(d.prop, d.val)
			continue
		}
		kept = append(kept, d)
	}
	if len(kept) == 0 {
		n.RemoveAttr("style")
		return
	}
	var b strings.Builder
	for i, d := range kept {
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(d.prop)
		b.WriteByte(':')
		b.WriteString(d.val)
	}
	if s := b.String(); s != v {
		n.SetAttr("style", s)
	}
}

func optimizeColorAttrs(n *dom.Node, refs *Refs) {
	for prop := range colorProps {
		if !n.HasAttr(prop) {
			continue
		}
		v, ok := n.AttrValue(prop)
		if !ok {
			continue
		}
		if isDroppableDefault(n, refs, prop, strings.ToLower(strings.TrimSpace(v))) {
			n.RemoveAttr(prop)
			continue
		}
		if s := shortestColor(v); s != v {
			n.SetAttr(prop, s)
		}
	}
	for prop, defaults := range defaultValues {
		if colorProps[prop] || !n.HasAttr(prop) {
			continue
		}
		if v, ok := n.AttrValue(prop); ok {
			norm := strings.ToLower(strings.TrimSpace(v))
			for _, d := range defaults {
				if norm == d && isDroppableDefault(n, refs, prop, norm) {
					n.RemoveAttr(prop)
					break
				}
			}
		}
	}
}

// isDroppableDefault reports whether prop:val is the property's initial
// value and removing it provably changes nothing.
func isDroppableDefault(n *dom.Node, refs *Refs, prop, val string) bool {
	defaults, known := defaultValues[prop]
	if !known {
		return false
	}
	match := false
	for _, d := range defaults {
		if val == d {
			match = true
			break
		}
	}
	if !match {
		return false
	}
	if !inheritedProps[prop] {
		return true
	}
	// The default was possibly masking an inherited value.
	if refs.HasStylesheet || refs.HasUse {
		return false
	}
	for e := n.Parent; e != nil && e.Kind == dom.KindElement; e = e.Parent {
		if e.HasAttr(prop) {
			return false
		}
		if e.HasAttr("style") {
			sv, ok := e.AttrValue("style")
			if !ok || strings.Contains(sv, prop) {
				return false
			}
		}
	}
	return true
}

type decl struct{ prop, val string }

// parseDecls splits an inline style into declarations. Anything beyond the
// simple `prop: value; ...` shape (comments, quotes, braces, at-rules)
// reports false and the style stays untouched. url(...) values are fine.
func parseDecls(s string) ([]decl, bool) {
	if strings.ContainsAny(s, `"'{}\/`) {
		return nil, false
	}
	var out []decl
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		i := strings.IndexByte(part, ':')
		if i <= 0 {
			return nil, false
		}
		prop := strings.ToLower(strings.TrimSpace(part[:i]))
		val := strings.TrimSpace(part[i+1:])
		if prop == "" || val == "" || strings.ContainsAny(prop, " \t(") {
			return nil, false
		}
		out = append(out, decl{prop, val})
	}
	return out, true
}

// shortNames maps colors to keywords shorter than any hex form.
var shortNames = map[uint32]string{
	0xff0000: "red",
	0xd2b48c: "tan",
}

// namedColors are keywords we can resolve to RGB (extend as needed; unknown
// names simply stay as written).
var namedColors = map[string]uint32{
	"black": 0x000000, "white": 0xffffff, "red": 0xff0000, "green": 0x008000,
	"blue": 0x0000ff, "yellow": 0xffff00, "cyan": 0x00ffff, "aqua": 0x00ffff,
	"magenta": 0xff00ff, "fuchsia": 0xff00ff, "gray": 0x808080,
	"grey": 0x808080, "silver": 0xc0c0c0, "maroon": 0x800000,
	"olive": 0x808000, "lime": 0x00ff00, "teal": 0x008080, "navy": 0x000080,
	"purple": 0x800080, "orange": 0xffa500, "tan": 0xd2b48c,
}

// shortestColor rewrites a color value in its shortest spelling; anything it
// does not fully understand comes back unchanged. Ties keep the input.
func shortestColor(v string) string {
	in := strings.TrimSpace(v)
	rgb, ok := parseColor(strings.ToLower(in))
	if !ok {
		return v
	}
	best := hexForm(rgb)
	if name, ok := shortNames[rgb]; ok && len(name) < len(best) {
		best = name
	}
	if len(best) < len(in) {
		return best
	}
	return v
}

func hexForm(rgb uint32) string {
	r, g, b := byte(rgb>>16), byte(rgb>>8), byte(rgb)
	const hexdigit = "0123456789abcdef"
	if r>>4 == r&0xf && g>>4 == g&0xf && b>>4 == b&0xf {
		return string([]byte{'#', hexdigit[r&0xf], hexdigit[g&0xf], hexdigit[b&0xf]})
	}
	return string([]byte{'#',
		hexdigit[r>>4], hexdigit[r&0xf],
		hexdigit[g>>4], hexdigit[g&0xf],
		hexdigit[b>>4], hexdigit[b&0xf]})
}

func parseColor(s string) (uint32, bool) {
	if rgb, ok := namedColors[s]; ok {
		return rgb, true
	}
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		switch len(h) {
		case 3:
			v, err := strconv.ParseUint(h, 16, 32)
			if err != nil {
				return 0, false
			}
			r, g, b := (v>>8)&0xf, (v>>4)&0xf, v&0xf
			return uint32(r<<20 | r<<16 | g<<12 | g<<8 | b<<4 | b), true
		case 6:
			v, err := strconv.ParseUint(h, 16, 32)
			if err != nil {
				return 0, false
			}
			return uint32(v), true
		}
		return 0, false
	}
	if strings.HasPrefix(s, "rgb(") && strings.HasSuffix(s, ")") {
		parts := strings.Split(s[4:len(s)-1], ",")
		if len(parts) != 3 {
			return 0, false
		}
		var rgb uint32
		for _, p := range parts {
			n, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil || n < 0 || n > 255 {
				return 0, false
			}
			rgb = rgb<<8 | uint32(n)
		}
		return rgb, true
	}
	return 0, false
}
