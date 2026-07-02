package path

import "testing"

func TestSerializeMinimalSeparators(t *testing.T) {
	cases := []struct {
		in   []Cmd
		want string
	}{
		{cmds(c('M', 0, 0), c('L', 10, 10)), "M0 0 10 10"},
		{cmds(c('M', 0, 0), c('L', -10, 10)), "M0 0-10 10"},
		{cmds(c('M', .5, .5)), "M.5.5"},
		{cmds(c('M', 0, 0), c('l', .5, .5)), "M0 0l.5.5"},
		{cmds(c('M', 0, 0), c('L', 1.5, .5)), "M0 0 1.5.5"},
		{cmds(c('M', 0, 0), c('L', 15, .5)), "M0 0 15 .5"},
		{cmds(c('M', 0, 0), c('L', 100000, .5)), "M0 0 1e5.5"},
		{cmds(c('M', 0, 0), c('z')), "M0 0z"},
		{cmds(c('M', 0, 0), c('Z'), c('m', 5, 5), c('l', 1, 1)), "M0 0Zm5 5 1 1"},
		{cmds(c('M', 0, 0), c('H', 5), c('H', 6)), "M0 0H5 6"},
		{cmds(c('M', 0, 0), c('M', 5, 5)), "M0 0M5 5"},
		{cmds(c('M', 0, 0), c('a', 5, 5, 0, 0, 1, 10, 10)), "M0 0a5 5 0 0110 10"},
		{cmds(c('M', 0, 0), c('a', 5, 5, 0, 0, 1, -10, 10)), "M0 0a5 5 0 01-10 10"},
		{cmds(c('M', 0, 0), c('a', 5, 5, 0, 0, 1, .5, .5)), "M0 0a5 5 0 01.5.5"},
		{
			cmds(c('M', 0, 0), c('a', 1, 1, 0, 0, 1, 1, 1), c('a', 2, 2, 0, 0, 0, 2, 2)),
			"M0 0a1 1 0 011 1 2 2 0 002 2",
		},
		{cmds(c('M', 0, 0), c('C', 1, 2, 3, 4, 5, 6), c('S', 7, 8, 9, 10)), "M0 0C1 2 3 4 5 6S7 8 9 10"},
	}
	for _, tc := range cases {
		got := string(Serialize(nil, tc.in, -1))
		if got != tc.want {
			t.Errorf("Serialize(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Everything Serialize emits must parse back to the same command list.
func TestSerializeParsesBack(t *testing.T) {
	lists := [][]Cmd{
		cmds(c('M', 0, 0), c('L', 10, 10), c('z')),
		cmds(c('M', .5, .5), c('l', -.25, .75), c('a', 5, 5, 0, 1, 0, 2, 2)),
		cmds(c('M', 1e5, 1e-5), c('L', 0.30000000000000004, 3)),
		cmds(c('M', 0, 0), c('L', 100000, .5), c('L', 1e2, .5)),
	}
	for _, l := range lists {
		out := Serialize(nil, l, -1)
		back, err := Parse(out)
		if err != nil {
			t.Errorf("reparse %q: %v", out, err)
			continue
		}
		if !equalCmds(l, back) {
			t.Errorf("round-trip %q: %v != %v", out, l, back)
		}
	}
}
