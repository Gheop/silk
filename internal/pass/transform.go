package pass

import (
	"math"
	"strconv"

	"github.com/Gheop/silk/internal/dom"
	"github.com/Gheop/silk/internal/path"
)

// matrix is the affine transform (a b c d e f): x' = a·x + c·y + e.
type matrix struct{ a, b, c, d, e, f float64 }

var identity = matrix{a: 1, d: 1}

func (m matrix) mul(n matrix) matrix {
	return matrix{
		a: m.a*n.a + m.c*n.b,
		b: m.b*n.a + m.d*n.b,
		c: m.a*n.c + m.c*n.d,
		d: m.b*n.c + m.d*n.d,
		e: m.a*n.e + m.c*n.f + m.e,
		f: m.b*n.e + m.d*n.f + m.f,
	}
}

// transformAttrs is where transform lists appear.
var transformAttrs = []string{"transform", "gradientTransform", "patternTransform"}

// ConvertTransforms collapses each transform list into its shortest
// equivalent: nothing at all for the identity, a pure translate or scale
// when the matrix is one, a matrix otherwise — and only ever when that is
// byte-shorter than the original. Translation components are rounded to
// prec (they are positions); the linear factors are kept exact, since their
// error multiplies across the whole subtree.
func ConvertTransforms(doc *dom.Node, prec int) {
	doc.Walk(func(n *dom.Node) bool {
		if n.Kind != dom.KindElement {
			return true
		}
		for _, attr := range transformAttrs {
			if !n.HasAttr(attr) {
				continue
			}
			v, ok := n.AttrValue(attr)
			if !ok {
				continue
			}
			m, err := parseTransformList(v)
			if err != nil {
				continue
			}
			m.e = quantizeTo(m.e, prec)
			m.f = quantizeTo(m.f, prec)
			if m == identity {
				n.RemoveAttr(attr)
				continue
			}
			if s := shortestTransform(m, prec); len(s) < len(v) {
				n.SetAttr(attr, s)
			}
		}
		return true
	})
}

func quantizeTo(v float64, prec int) float64 {
	s, err := strconv.ParseFloat(string(path.FormatNumber(nil, v, prec)), 64)
	if err != nil {
		return v
	}
	return s
}

func shortestTransform(m matrix, prec int) string {
	var b []byte
	switch {
	case m.b == 0 && m.c == 0 && m.a == 1 && m.d == 1:
		b = append(b, "translate("...)
		b = path.FormatNumber(b, m.e, prec)
		if m.f != 0 {
			b = append(b, ' ')
			b = path.FormatNumber(b, m.f, prec)
		}
	case m.b == 0 && m.c == 0 && m.e == 0 && m.f == 0:
		b = append(b, "scale("...)
		b = path.FormatNumber(b, m.a, -1)
		if m.d != m.a {
			b = append(b, ' ')
			b = path.FormatNumber(b, m.d, -1)
		}
	default:
		b = append(b, "matrix("...)
		for i, v := range []float64{m.a, m.b, m.c, m.d} {
			if i > 0 {
				b = append(b, ' ')
			}
			b = path.FormatNumber(b, v, -1)
		}
		b = append(b, ' ')
		b = path.FormatNumber(b, m.e, prec)
		b = append(b, ' ')
		b = path.FormatNumber(b, m.f, prec)
	}
	return string(append(b, ')'))
}

// parseTransformList parses and multiplies out an SVG transform list. Any
// syntax it does not fully understand is an error: the attribute then stays
// byte-untouched.
func parseTransformList(s string) (matrix, error) {
	p := &tparser{s: s}
	m := identity
	p.skipWsp()
	for !p.eof() {
		name := p.ident()
		p.skipWsp()
		if !p.expect('(') {
			return m, tsyntaxError{}
		}
		var args []float64
		for {
			p.skipSep()
			if p.eof() {
				return m, tsyntaxError{}
			}
			if p.peek() == ')' {
				p.pos++
				break
			}
			v, err := p.number()
			if err != nil {
				return m, err
			}
			args = append(args, v)
		}
		next, err := transformOf(name, args)
		if err != nil {
			return m, err
		}
		m = m.mul(next)
		p.skipSep()
	}
	return m, nil
}

type tsyntaxError struct{}

func (tsyntaxError) Error() string { return "pass: transform syntax error" }

func transformOf(name string, a []float64) (matrix, error) {
	switch name {
	case "translate":
		switch len(a) {
		case 1:
			return matrix{a: 1, d: 1, e: a[0]}, nil
		case 2:
			return matrix{a: 1, d: 1, e: a[0], f: a[1]}, nil
		}
	case "scale":
		switch len(a) {
		case 1:
			return matrix{a: a[0], d: a[0]}, nil
		case 2:
			return matrix{a: a[0], d: a[1]}, nil
		}
	case "rotate":
		if len(a) == 1 || len(a) == 3 {
			rad := a[0] * math.Pi / 180
			cos, sin := math.Cos(rad), math.Sin(rad)
			r := matrix{a: cos, b: sin, c: -sin, d: cos}
			if len(a) == 3 {
				t1 := matrix{a: 1, d: 1, e: a[1], f: a[2]}
				t2 := matrix{a: 1, d: 1, e: -a[1], f: -a[2]}
				return t1.mul(r).mul(t2), nil
			}
			return r, nil
		}
	case "skewX":
		if len(a) == 1 {
			return matrix{a: 1, d: 1, c: math.Tan(a[0] * math.Pi / 180)}, nil
		}
	case "skewY":
		if len(a) == 1 {
			return matrix{a: 1, d: 1, b: math.Tan(a[0] * math.Pi / 180)}, nil
		}
	case "matrix":
		if len(a) == 6 {
			return matrix{a[0], a[1], a[2], a[3], a[4], a[5]}, nil
		}
	}
	return matrix{}, tsyntaxError{}
}

type tparser struct {
	s   string
	pos int
}

func (p *tparser) eof() bool { return p.pos >= len(p.s) }

func (p *tparser) peek() byte {
	if p.eof() {
		return 0
	}
	return p.s[p.pos]
}

func (p *tparser) skipWsp() {
	for !p.eof() {
		switch p.s[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *tparser) skipSep() {
	p.skipWsp()
	if !p.eof() && p.s[p.pos] == ',' {
		p.pos++
		p.skipWsp()
	}
}

func (p *tparser) expect(c byte) bool {
	if p.peek() == c {
		p.pos++
		return true
	}
	return false
}

func (p *tparser) ident() string {
	start := p.pos
	for !p.eof() {
		c := p.s[p.pos]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			p.pos++
			continue
		}
		break
	}
	return p.s[start:p.pos]
}

func (p *tparser) number() (float64, error) {
	start := p.pos
	for !p.eof() {
		c := p.s[p.pos]
		if c >= '0' && c <= '9' || c == '.' || c == '-' || c == '+' || c == 'e' || c == 'E' {
			p.pos++
			continue
		}
		break
	}
	v, err := strconv.ParseFloat(p.s[start:p.pos], 64)
	if err != nil {
		return 0, tsyntaxError{}
	}
	return v, nil
}
