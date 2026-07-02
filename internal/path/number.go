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
		p := math.Pow10(prec)
		if r := math.Round(v*p) / p; !math.IsInf(r, 0) && !math.IsNaN(r) {
			v = r
		}
	}
	if v == 0 {
		return append(dst, '0') // also folds -0
	}
	f := minimizeDecimal(strconv.AppendFloat(nil, v, 'f', -1, 64))
	e := compactExponent(strconv.AppendFloat(nil, v, 'e', -1, 64))
	if len(e) < len(f) {
		return append(dst, e...)
	}
	return append(dst, f...)
}

// quantize returns the float64 the formatted text of v denotes; geometry
// tracking uses it so rounding error never accumulates across commands.
func quantize(v float64, prec int) float64 {
	q, err := strconv.ParseFloat(string(formatNumber(nil, v, prec)), 64)
	if err != nil {
		return v
	}
	return q
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
