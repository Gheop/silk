package path

import "testing"

func cmds(list ...Cmd) []Cmd { return list }
func c(op byte, args ...float64) Cmd {
	return Cmd{Op: op, Args: args}
}

func TestParseGrammar(t *testing.T) {
	cases := []struct {
		in   string
		want []Cmd
	}{
		{"M0 0", cmds(c('M', 0, 0))},
		{"m 10,20", cmds(c('m', 10, 20))},
		{"  M 0 , 0  ", cmds(c('M', 0, 0))},
		{"M0 0L10 10", cmds(c('M', 0, 0), c('L', 10, 10))},
		// Implicit repeats: extra M pairs are linetos of the same case.
		{"M0 0 10 10 20 20", cmds(c('M', 0, 0), c('L', 10, 10), c('L', 20, 20))},
		{"m0 0 10 10", cmds(c('m', 0, 0), c('l', 10, 10))},
		{"M0 0L1 1 2 2", cmds(c('M', 0, 0), c('L', 1, 1), c('L', 2, 2))},
		{"M0 0Z", cmds(c('M', 0, 0), c('Z'))},
		{"M0 0z", cmds(c('M', 0, 0), c('z'))},
		{"M0 0zm5 5z", cmds(c('M', 0, 0), c('z'), c('m', 5, 5), c('z'))},
		// Sign as separator.
		{"M10-20", cmds(c('M', 10, -20))},
		{"M0 0l+5+5", cmds(c('M', 0, 0), c('l', 5, 5))},
		// Dot as separator after a fractional number.
		{"M.5.5", cmds(c('M', .5, .5))},
		{"M5.5.5", cmds(c('M', 5.5, .5))},
		// Scientific notation.
		{"M0 0L1e2 1E-2", cmds(c('M', 0, 0), c('L', 100, .01))},
		{"M0 0h1e+2", cmds(c('M', 0, 0), c('h', 100))},
		// Exponents are integers: a dot ends the exponent and starts a new
		// number.
		{"M1e2.5-.5.5", cmds(c('M', 100, .5), c('L', -.5, .5))},
		// Shorthands and curves.
		{"M0 0H5V6", cmds(c('M', 0, 0), c('H', 5), c('V', 6))},
		{"M0 0C1 2 3 4 5 6S7 8 9 10", cmds(c('M', 0, 0), c('C', 1, 2, 3, 4, 5, 6), c('S', 7, 8, 9, 10))},
		{"M0 0Q1 2 3 4T5 6", cmds(c('M', 0, 0), c('Q', 1, 2, 3, 4), c('T', 5, 6))},
		// Arcs: flags are single digits and may be packed without separators.
		{"M0 0a5 5 0 01 10 10", cmds(c('M', 0, 0), c('a', 5, 5, 0, 0, 1, 10, 10))},
		{"M0 0a5 5 0 0110 10", cmds(c('M', 0, 0), c('a', 5, 5, 0, 0, 1, 10, 10))},
		{"M0 0a5,5,0,0,1,10,10", cmds(c('M', 0, 0), c('a', 5, 5, 0, 0, 1, 10, 10))},
		{"M0 0A5 5 0 1 0 -10-10", cmds(c('M', 0, 0), c('A', 5, 5, 0, 1, 0, -10, -10))},
		{"M0 0a5 5 0 01.5.5", cmds(c('M', 0, 0), c('a', 5, 5, 0, 0, 1, .5, .5))},
		// Arc repeat groups.
		{"M0 0a1 1 0 011 1 2 2 0 002 2", cmds(c('M', 0, 0), c('a', 1, 1, 0, 0, 1, 1, 1), c('a', 2, 2, 0, 0, 0, 2, 2))},
		// "5." is invalid per the strict grammar but accepted by renderers;
		// be lenient on input (we always emit the strict form).
		{"M0 0l5. 5", cmds(c('M', 0, 0), c('l', 5, 5))},
		{"M0 0l5.-5", cmds(c('M', 0, 0), c('l', 5, -5))},
	}
	for _, tc := range cases {
		got, err := Parse([]byte(tc.in))
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.in, err)
			continue
		}
		if !equalCmds(got, tc.want) {
			t.Errorf("Parse(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\t"} {
		got, err := Parse([]byte(in))
		if err != nil || len(got) != 0 {
			t.Errorf("Parse(%q) = %v, %v; want empty, nil", in, got, err)
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := []string{
		"L10 10",              // must start with moveto
		"M",                   // missing args
		"M0",                  // incomplete pair
		"M0 0L",               // command without args
		"M0 0 5",              // dangling implicit arg
		"M1e2.5 0",            // dangling arg after exponent-dot split
		"M0 0X5 5",            // unknown command
		"M0,,0",               // double comma
		"M0 0,",               // trailing comma
		",M0 0",               // leading comma
		"M0 0a5 5 0 2 1 1 1",  // large-arc flag not 0/1
		"M0 0a5 5 0 0 5 1 1",  // sweep flag not 0/1
		"M0 0a5 5 0 .5 1 1 1", // flag cannot be fractional
		"M1e999 0",            // exponent overflow
		"M0 0\x00",            // hostile byte
		"M0 0\xc3\xa9",        // non-ASCII
		"M--5 0",              // double sign
		"M.e5 0",              // mantissa without digits
		"M5e 0",               // exponent without digits
		"M0 0z5 5",            // closepath takes no implicit repeat
	}
	for _, in := range cases {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("Parse(%q): expected error", in)
		}
	}
}

func equalCmds(a, b []Cmd) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Op != b[i].Op || len(a[i].Args) != len(b[i].Args) {
			return false
		}
		for j := range a[i].Args {
			if a[i].Args[j] != b[i].Args[j] {
				return false
			}
		}
	}
	return true
}
