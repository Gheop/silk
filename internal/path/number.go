package path

import (
	"bytes"
	"math"
	"strconv"
)

// formatNumber appends the shortest textual form of v. prec < 0 keeps the
// exact value (shortest string that round-trips the float64); prec >= 0
// rounds half away from zero to that many decimals first. The result never
// carries a sign on zero, a leading "0." zero, redundant "+", or exponent
// padding, and it always re-parses to the same float64 it was printed from.
func formatNumber(dst []byte, v float64, prec int) []byte {
	if prec >= 0 && prec <= 15 {
		p := pow10[prec]
		if r := math.Round(v*p) / p; !math.IsInf(r, 0) && !math.IsNaN(r) {
			v = r
		}
	}
	if v == 0 {
		return append(dst, '0') // also folds -0
	}
	// The exponent form can only win outside [1e-2, 1e5): within it, the
	// mantissa digits alone match the decimal form and "e±x" is pure
	// overhead. Skipping the second formatting halves the hot path.
	abs := math.Abs(v)
	if abs >= 1e-2 && abs < 1e5 {
		// The scaled value must stay within exact int64/float64 integer
		// range; at precision 15 a coordinate near 1e5 would overflow.
		if prec >= 0 && prec <= 15 && abs*pow10[prec] < 9e15 {
			// v is already on the 10^-prec grid; its exact decimal has at
			// most prec fraction digits, and no shorter decimal exists
			// within half an ulp, so integer formatting reproduces the
			// shortest round-trip form directly.
			return appendScaledDecimal(dst, int64(math.Round(v*pow10[prec])), prec)
		}
		mark := len(dst)
		dst = strconv.AppendFloat(dst, v, 'f', -1, 64)
		return minimizeDecimalAt(dst, mark)
	}
	f := minimizeDecimal(strconv.AppendFloat(nil, v, 'f', -1, 64))
	e := compactExponent(strconv.AppendFloat(nil, v, 'e', -1, 64))
	if len(e) < len(f) {
		return append(dst, e...)
	}
	return append(dst, f...)
}

// minimizeDecimalAt applies the "0.5" -> ".5" rewrite in place to the number
// that starts at index mark.
func minimizeDecimalAt(b []byte, mark int) []byte {
	n := b[mark:]
	switch {
	case len(n) > 2 && n[0] == '0' && n[1] == '.':
		return append(b[:mark], n[1:]...)
	case len(n) > 3 && n[0] == '-' && n[1] == '0' && n[2] == '.':
		b[mark+1] = '-'
		return append(b[:mark], n[1:]...)
	}
	return b
}

// smallDigits holds "00".."99": a two-digit lookup halves the divisions of
// decimal formatting.
const smallDigits = "00010203040506070809" +
	"10111213141516171819" +
	"20212223242526272829" +
	"30313233343536373839" +
	"40414243444546474849" +
	"50515253545556575859" +
	"60616263646566676869" +
	"70717273747576777879" +
	"80818283848586878889" +
	"90919293949596979899"

// pow10i[n] is the smallest n+1-digit integer: digit counting compares
// against it instead of dividing.
var pow10i = [...]int64{1, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18}

var pow10 = [16]float64{1, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10, 1e11, 1e12, 1e13, 1e14, 1e15}

// quantize returns the float64 the formatted text of v denotes; geometry
// tracking uses it so rounding error never accumulates across commands. It
// mirrors formatNumber arithmetically: the shortest round-trip formatting of
// the rounded value parses back to exactly that value.
func quantize(v float64, prec int) float64 {
	if prec < 0 || prec > 15 {
		return v
	}
	p := pow10[prec]
	r := math.Round(v*p) / p
	if math.IsInf(r, 0) || math.IsNaN(r) {
		return v
	}
	if r == 0 {
		return 0 // formatNumber emits "0" for negative zero too
	}
	return r
}

// numShape describes the exact text formatNumber will emit for v at prec,
// without emitting it. ok is false when the fast decimal path does not
// apply; the caller must then really format.
type numShape struct {
	length    int
	headMinus bool // first byte is '-'
	headDot   bool // first non-sign byte is '.'
	hasDot    bool // text contains '.' (it never contains 'e' on this path)
}

func numInfo(v float64, prec int) (numShape, bool) {
	_, k, fast := gridOf(v, prec)
	if !fast {
		return numShape{}, false
	}
	return shapeOf(k, prec), true
}

// gnum is one number rounded once: the value its text denotes and, on the
// fast decimal path, the integer on the 10^-prec grid and the exact shape
// formatNumber would emit. The candidate encodings of one command share
// these instead of rounding the same coordinate once per candidate and then
// again to measure it, emit it and track what it denotes.
type gnum struct {
	q  float64 // quantize(v, prec): what the text denotes
	rk float64 // the grid integer as a float, from the same single rounding; NaN off-grid
	// Completed on first measurement only: most arguments are pruned before.
	k     int64
	shape numShape
	state uint8 // gRounded, gFast or gSlow
}

const (
	gRounded = iota // q and rk only
	gFast           // k and shape valid: text = appendScaledDecimal(k, prec)
	gSlow           // formatNumber must really format
)

// roundOnce is quantize, also returning the grid integer the rounding went
// through (NaN when there is no grid: exact precision or overflow).
func roundOnce(v float64, prec int) (q, rk float64) {
	if prec < 0 || prec > 15 {
		return v, math.NaN()
	}
	p := pow10[prec]
	rk = math.Round(v * p)
	r := rk / p
	if math.IsInf(r, 0) || math.IsNaN(r) {
		return v, math.NaN()
	}
	if r == 0 {
		return 0, 0 // formatNumber emits "0" for negative zero too
	}
	return r, rk
}

// complete finishes g the way gridOf would have, from its stored rounding.
func (g *gnum) complete(prec int) {
	if math.IsNaN(g.rk) {
		g.state = gSlow
		return
	}
	if g.q == 0 {
		g.k, g.shape, g.state = 0, numShape{length: 1}, gFast
		return
	}
	p := pow10[prec]
	abs := math.Abs(g.q)
	if abs < 1e-2 || abs >= 1e5 || abs*p >= 9e15 {
		g.state = gSlow
		return
	}
	if g.rk < 2e15 && g.rk > -2e15 {
		g.k = int64(g.rk)
	} else {
		g.k = int64(math.Round(g.q * p))
	}
	g.shape, g.state = shapeOf(g.k, prec), gFast
}

// gridOf rounds v once to prec decimals and returns what the text denotes
// (quantize's result) and, when formatNumber takes its fast decimal path,
// the integer k with text = appendScaledDecimal(k, prec).
func gridOf(v float64, prec int) (q float64, k int64, fast bool) {
	if prec < 0 || prec > 15 {
		return v, 0, false
	}
	p := pow10[prec]
	vp := v * p
	rk := math.Round(vp) // the grid integer, as a float
	r := rk / p
	if math.IsInf(r, 0) || math.IsNaN(r) {
		return v, 0, false
	}
	if r == 0 {
		return 0, 0, true // "0", also for -0
	}
	abs := math.Abs(r)
	if abs < 1e-2 || abs >= 1e5 || abs*p >= 9e15 {
		return r, 0, false
	}
	// r is rk/p; multiplying back rounds once more, and for |rk| below
	// 2^51 that double rounding stays under half a unit, so the integer is
	// rk itself and the second Round is skipped. Larger magnitudes keep the
	// exact two-step form formatNumber uses.
	if rk < 2e15 && rk > -2e15 {
		return r, int64(rk), true
	}
	return r, int64(math.Round(r * p)), true
}

// shapeOf describes appendScaledDecimal(k, prec) without formatting it.
func shapeOf(k int64, prec int) numShape {
	if k == 0 {
		return numShape{length: 1}
	}
	neg := k < 0
	if neg {
		k = -k
	}
	// Trailing zeros first (typically none or one), then the digit count by
	// comparison against a power table: divisions cost ~25 cycles each and
	// this loop ran one per digit for every candidate argument.
	tz, x := 0, k
	for x%10 == 0 {
		x /= 10
		tz++
	}
	nd := tz + 1
	for nd-tz < len(pow10i) && x >= pow10i[nd-tz] {
		nd++
	}
	s := numShape{headMinus: neg}
	n := 0
	if neg {
		n++
	}
	switch {
	case prec == 0:
		n += nd
	case nd > prec:
		// Integer digits, then the trimmed fraction (the prec last digits).
		n += nd - prec
		if frac := prec - min(tz, prec); frac > 0 {
			s.hasDot = true
			n += 1 + frac
		}
	default:
		// Pure fraction: ".", zero padding, then the trimmed digits.
		s.headDot, s.hasDot = true, true
		n += 1 + (prec - nd) + (nd - tz)
	}
	s.length = n
	return s
}

// appendScaledDecimal formats k/10^prec in minimal decimal form.
func appendScaledDecimal(dst []byte, k int64, prec int) []byte {
	if k == 0 {
		return append(dst, '0')
	}
	if k < 0 {
		dst = append(dst, '-')
		k = -k
	}
	var buf [20]byte
	i := len(buf)
	// Two digits per division, strconv style: the divide dominates here.
	for k >= 100 {
		q := k / 100
		r := 2 * (k - q*100)
		i -= 2
		buf[i], buf[i+1] = smallDigits[r], smallDigits[r+1]
		k = q
	}
	if k >= 10 {
		i -= 2
		buf[i], buf[i+1] = smallDigits[2*k], smallDigits[2*k+1]
	} else {
		i--
		buf[i] = byte('0' + k)
	}
	digits := buf[i:]
	if prec == 0 {
		return append(dst, digits...)
	}
	frac := digits
	intPart := []byte(nil)
	if len(digits) > prec {
		intPart = digits[:len(digits)-prec]
		frac = digits[len(digits)-prec:]
	}
	for len(frac) > 0 && frac[len(frac)-1] == '0' {
		frac = frac[:len(frac)-1]
	}
	dst = append(dst, intPart...)
	if len(frac) == 0 {
		if len(intPart) == 0 {
			return append(dst, '0')
		}
		return dst
	}
	dst = append(dst, '.')
	for range prec - len(digits) {
		dst = append(dst, '0')
	}
	return append(dst, frac...)
}

// minimizeDecimal rewrites "0.5" as ".5" and "-0.5" as "-.5".
func minimizeDecimal(f []byte) []byte {
	if len(f) > 2 && f[0] == '0' && f[1] == '.' {
		return f[1:]
	}
	if len(f) > 3 && f[0] == '-' && f[1] == '0' && f[2] == '.' {
		f = f[1:]
		f[0] = '-'
		return f
	}
	return f
}

// compactExponent rewrites strconv's "1.25e-07" as "1.25e-7" and "5e+06" as
// "5e6".
func compactExponent(e []byte) []byte {
	i := bytes.IndexByte(e, 'e')
	if i < 0 {
		return e
	}
	out := e[:i+1]
	exp := e[i+1:]
	if exp[0] == '-' {
		out = append(out, '-')
		exp = exp[1:]
	} else if exp[0] == '+' {
		exp = exp[1:]
	}
	for len(exp) > 1 && exp[0] == '0' {
		exp = exp[1:]
	}
	return append(out, exp...)
}

// FormatNumber appends the shortest textual form of v, rounding to prec
// decimals when prec >= 0. Exported for the transform pass, which shares the
// number-minimization rules.
func FormatNumber(dst []byte, v float64, prec int) []byte {
	return formatNumber(dst, v, prec)
}
