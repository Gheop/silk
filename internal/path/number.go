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
		if prec >= 0 && prec <= 15 {
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
	for k > 0 {
		i--
		buf[i] = byte('0' + k%10)
		k /= 10
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
