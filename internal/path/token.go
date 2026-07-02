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
	var out []Cmd
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
		args := make([]float64, n)
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
	if c := s.peek(); c == '+' || c == '-' {
		s.pos++
	}
	intDigits := s.digits()
	fracDigits := 0
	if s.peek() == '.' {
		s.pos++
		fracDigits = s.digits()
	}
	if intDigits == 0 && fracDigits == 0 {
		return 0, s.errf("expected number")
	}
	if c := s.peek(); c == 'e' || c == 'E' {
		s.pos++
		if c := s.peek(); c == '+' || c == '-' {
			s.pos++
		}
		if s.digits() == 0 {
			return 0, s.errf("exponent without digits")
		}
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
	return v, nil
}

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
