package path

import "testing"

func TestFormatNumberExact(t *testing.T) {
	cases := []struct {
		v    float64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{-1, "-1"},
		{0.5, ".5"},
		{-0.5, "-.5"},
		{1.5, "1.5"},
		{1.10, "1.1"},
		{12345, "12345"},
		{100000, "1e5"},
		{-100000, "-1e5"},
		{123456789, "123456789"},
		{0.001, ".001"},
		{0.0001, "1e-4"},
		{-0.0001, "-1e-4"},
		{1e21, "1e21"},
		{1.25e-7, "1.25e-7"},
	}
	for _, tc := range cases {
		got := string(formatNumber(nil, tc.v, -1))
		if got != tc.want {
			t.Errorf("formatNumber(%v, exact) = %q, want %q", tc.v, got, tc.want)
		}
	}
}

func TestFormatNumberRounded(t *testing.T) {
	cases := []struct {
		v    float64
		prec int
		want string
	}{
		{1.23456, 3, "1.235"},
		{1.23444, 3, "1.234"},
		{1.0004, 3, "1"},
		{-1.0004, 3, "-1"},
		{0.4996, 3, ".5"},
		{-0.0004, 3, "0"}, // never emit -0
		{2.5, 0, "3"},     // round half away from zero
		{-2.5, 0, "-3"},
		{1.5, 0, "2"},
		{123456.789, 2, "123456.79"},
		{100000.4, 0, "1e5"},
	}
	for _, tc := range cases {
		got := string(formatNumber(nil, tc.v, tc.prec))
		if got != tc.want {
			t.Errorf("formatNumber(%v, %d) = %q, want %q", tc.v, tc.prec, got, tc.want)
		}
	}
}

func TestQuantizeMatchesFormat(t *testing.T) {
	for _, v := range []float64{0, 1.23456, -987.654321, 0.000123, 12345.6789} {
		for _, prec := range []int{-1, 0, 1, 2, 3, 6} {
			q := quantize(v, prec)
			// Formatting the quantized value must reproduce the same bytes:
			// the emitted text denotes exactly q.
			a := string(formatNumber(nil, v, prec))
			b := string(formatNumber(nil, q, prec))
			if a != b {
				t.Errorf("quantize(%v, %d): format mismatch %q vs %q", v, prec, a, b)
			}
		}
	}
}

// numInfo must describe formatNumber's output exactly whenever it claims to.
func TestNumInfoMatchesFormat(t *testing.T) {
	rng := []float64{0, 1, -1, .5, -.5, .01, -.01, .009, 99999.999, -99999.5,
		1234.5678, .125, 100, -100, 10.01, .0999, 3, 33.3, 0.30000000000000004}
	for _, prec := range []int{0, 1, 2, 3, 5, 9, 12, 15} {
		for _, base := range rng {
			for _, mul := range []float64{1, 3.7, 12.34, 0.313, 999.99} {
				v := base * mul
				s, ok := numInfo(v, prec)
				if !ok {
					continue
				}
				text := formatNumber(nil, v, prec)
				if s.length != len(text) {
					t.Fatalf("numInfo(%v,%d).length=%d, text %q (%d)", v, prec, s.length, text, len(text))
				}
				if s.headMinus != (text[0] == '-') {
					t.Fatalf("numInfo(%v,%d).headMinus wrong for %q", v, prec, text)
				}
				headDot := text[0] == '.' || (text[0] == '-' && len(text) > 1 && text[1] == '.')
				if s.headDot != headDot {
					t.Fatalf("numInfo(%v,%d).headDot wrong for %q", v, prec, text)
				}
				hasDot := false
				for _, c := range text {
					if c == '.' {
						hasDot = true
					}
					if c == 'e' {
						t.Fatalf("fast path emitted exponent: %q", text)
					}
				}
				if s.hasDot != hasDot {
					t.Fatalf("numInfo(%v,%d).hasDot wrong for %q", v, prec, text)
				}
			}
		}
	}
}
