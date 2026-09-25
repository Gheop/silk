// Package path parses, optimizes, and re-serializes SVG path data. Parsing is
// lenient where renderers are lenient; serialization always emits the strict
// grammar. Any parse error means the caller keeps the original bytes.
package path

import (
	"errors"
	"fmt"
	"strconv"
)

// Cmd is one path command with its full argument group. Implicit repeats are
// expanded during parsing: every Cmd carries an explicit Op, and the extra
// coordinate pairs of a moveto become lineto commands of the same case.
type Cmd struct {
	Op   byte // one of MmZzLlHhVvCcSsQqTtAa
	Args []float64
}

func (c Cmd) String() string {
	return fmt.Sprintf("%c%v", c.Op, c.Args)
}

func arity(op byte) int {
	switch op | 0x20 {
	case 'm', 'l', 't':
		return 2
	case 'h', 'v':
		return 1
	case 'c':
		return 6
	case 's', 'q':
		return 4
	case 'a':
		return 7
	case 'z':
		return 0
	}
	return -1
}

func isCmdLetter(b byte) bool { return arity(b) >= 0 }

func isNumStart(b byte) bool {
	return b >= '0' && b <= '9' || b == '-' || b == '+' || b == '.'
}

// nextImplicit returns the operation of a letterless repeat group following
// op. After a moveto the implicit operation is a lineto of the same case;
// closepath admits no repeat at all.
func nextImplicit(op byte) byte {
	switch op {
	case 'M':
		return 'L'
	case 'm':
		return 'l'
	case 'Z', 'z':
		return 0
	}
	return op
}

// Parse converts path data into a command list. An empty or whitespace-only
// input yields an empty list. Any syntax error, unknown byte, or numeric
// overflow returns an error so the caller leaves the attribute untouched.
func Parse(d []byte) ([]Cmd, error) {
	s := scanner{d: d}
	s.skipWsp()
	if s.eof() {
		return nil, nil
	}
	// ~11 bytes per command is typical for real-world path data.
	out := make([]Cmd, 0, len(d)/10+4)
	var arena []float64 // shared backing for Args: one allocation per block
	// First block sized to the input so tiny paths do not pay for a big one.
	arenaBlock := min(4096, len(d)/5+8)
	var implicit byte
	sawComma := false
	for !s.eof() {
		b := s.peek()
		var op byte
		switch {
		case isCmdLetter(b):
			if sawComma {
				return nil, s.errf("comma before command")
			}
			op = b
			s.pos++
			s.skipWsp()
		case implicit != 0 && isNumStart(b):
			op = implicit
		default:
			return nil, s.errf("unexpected byte %q", b)
		}
		if len(out) == 0 && op != 'M' && op != 'm' {
			return nil, errors.New("path: must begin with moveto")
		}
		n := arity(op)
		if len(arena) < n {
			arena = make([]float64, max(arenaBlock, n))
			arenaBlock = 4096
		}
		args := arena[:n:n]
		arena = arena[n:]
		for i := range n {
			if i > 0 {
				if err := s.commaWsp(); err != nil {
					return nil, err
				}
			}
			var v float64
			var err error
			if (op|0x20) == 'a' && (i == 3 || i == 4) {
				v, err = s.flag()
			} else {
				v, err = s.number()
			}
			if err != nil {
				return nil, err
			}
			args[i] = v
		}
		out = append(out, Cmd{Op: op, Args: args})
		implicit = nextImplicit(op)
		sawComma = s.commaWspOpt()
	}
	if sawComma {
		return nil, errors.New("path: trailing comma")
	}
	return out, nil
}

type scanner struct {
	d   []byte
	pos int
}

func (s *scanner) eof() bool { return s.pos >= len(s.d) }

func (s *scanner) peek() byte {
	if s.eof() {
		return 0
	}
	return s.d[s.pos]
}

func (s *scanner) errf(format string, args ...any) error {
	return fmt.Errorf("path: %s at offset %d", fmt.Sprintf(format, args...), s.pos)
}

func isWsp(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

func (s *scanner) skipWsp() {
	for !s.eof() && isWsp(s.d[s.pos]) {
		s.pos++
	}
}

// commaWsp consumes the mandatory separation between two coordinates:
// whitespace and at most one comma. A sign or dot also separates, so an empty
// match is fine as long as a number can start here.
func (s *scanner) commaWsp() error {
	if s.commaWspOpt() {
		if s.eof() || !isNumStart(s.peek()) {
			return s.errf("dangling comma")
		}
	}
	return nil
}

// commaWspOpt consumes whitespace and at most one comma, reporting whether a
// comma was present.
func (s *scanner) commaWspOpt() bool {
	s.skipWsp()
	if !s.eof() && s.d[s.pos] == ',' {
		s.pos++
		s.skipWsp()
		return true
	}
	return false
}

func (s *scanner) digits() int {
	n := 0
	for !s.eof() && s.d[s.pos] >= '0' && s.d[s.pos] <= '9' {
		s.pos++
		n++
	}
	return n
}

func (s *scanner) number() (float64, error) {
	start := s.pos
	neg := false
	if c := s.peek(); c == '+' || c == '-' {
		neg = c == '-'
		s.pos++
	}
	var mant uint64
	exact := true
	intDigits := s.digitsAcc(&mant, &exact)
	fracDigits := 0
	if s.peek() == '.' {
		s.pos++
		fracDigits = s.digitsAcc(&mant, &exact)
	}
	if intDigits == 0 && fracDigits == 0 {
		return 0, s.errf("expected number")
	}
	hasExp := false
	if c := s.peek(); c == 'e' || c == 'E' {
		hasExp = true
		s.pos++
		if c := s.peek(); c == '+' || c == '-' {
			s.pos++
		}
		if s.digits() == 0 {
			return 0, s.errf("exponent without digits")
		}
	}
	// Fast path, bit-identical to strconv: a mantissa below 2^53 and a
	// power of ten up to 1e22 are both exact in float64, so one correctly
	// rounded division yields the correctly rounded result strconv would.
	// The scan above already validated the syntax; this spares a second
	// scan of every coordinate.
	if exact && !hasExp && mant < 1<<53 && fracDigits <= 22 {
		v := float64(mant)
		if fracDigits > 0 {
			v /= pow10f[fracDigits]
		}
		if neg {
			v = -v
		}
		if v > maxMagnitude || v < -maxMagnitude {
			return 0, s.errf("number %s beyond %g", s.d[start:s.pos], maxMagnitude)
		}
		return v, nil
	}
	text := s.d[start:s.pos]
	// Renderers accept a trailing dot ("5."); strconv wants digits or none.
	if text[len(text)-1] == '.' {
		text = text[:len(text)-1]
	}
	v, err := strconv.ParseFloat(string(text), 64)
	if err != nil {
		return 0, s.errf("number %q: %v", text, err)
	}
	if v > maxMagnitude || v < -maxMagnitude {
		return 0, s.errf("number %q beyond %g", text, maxMagnitude)
	}
	return v, nil
}

// maxMagnitude bounds accepted coordinates. Relative-to-absolute sums and
// arc geometry on values near the float64 range overflow to ±Inf, which the
// formatter would then write into the document ("h+Inf"); far below that,
// float32 renderers have already lost every fractional digit. A path beyond
// the bound is a parse error, so the attribute stays untouched.
const maxMagnitude = 1e15

// digitsAcc consumes digits like digits, accumulating their value into mant;
// exact is cleared once the accumulation could no longer be trusted.
func (s *scanner) digitsAcc(mant *uint64, exact *bool) int {
	n := 0
	for !s.eof() && s.d[s.pos] >= '0' && s.d[s.pos] <= '9' {
		if *mant < 1e18 {
			*mant = *mant*10 + uint64(s.d[s.pos]-'0')
		} else {
			*exact = false
		}
		s.pos++
		n++
	}
	return n
}

// pow10f[n] = 10^n, exact in float64 up to 1e22.
var pow10f = [...]float64{1, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10, 1e11,
	1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19, 1e20, 1e21, 1e22}

// flag reads an arc flag: exactly one '0' or '1', no sign, no fraction.
func (s *scanner) flag() (float64, error) {
	switch s.peek() {
	case '0':
		s.pos++
		return 0, nil
	case '1':
		s.pos++
		return 1, nil
	}
	return 0, s.errf("expected arc flag")
}
