package path

import "bytes"

// Serialize appends the shortest strict-grammar encoding of the command list:
// implicit repeat groups whenever the previous command allows them, and a
// separator only where the next token could otherwise extend the previous
// one. Arc flags are single digits, so they pack against whatever follows.
func Serialize(dst []byte, cs []Cmd, prec int) []byte {
	e := emitter{b: dst}
	var implicit byte
	for _, c := range cs {
		if c.Op != implicit || len(c.Args) == 0 {
			e.letter(c.Op)
		}
		arc := c.Op|0x20 == 'a'
		for i, v := range c.Args {
			if arc && (i == 3 || i == 4) {
				e.flag(v)
			} else {
				e.number(v, prec)
			}
		}
		implicit = nextImplicit(c.Op)
	}
	return e.b
}

type emitter struct {
	b        []byte
	prevKind byte // 0 start, 'l' letter, 'n' number, 'f' flag
	prevOpen bool // previous number contains '.' or an exponent: a leading
	// dot cannot extend it, so ".5" may follow unseparated
}

func (e *emitter) letter(op byte) {
	e.b = append(e.b, op)
	e.prevKind = 'l'
}

func (e *emitter) flag(v float64) {
	// A flag is one digit; only a directly preceding number could absorb it.
	if e.prevKind == 'n' {
		e.b = append(e.b, ' ')
	}
	if v != 0 {
		e.b = append(e.b, '1')
	} else {
		e.b = append(e.b, '0')
	}
	e.prevKind = 'f'
}

func (e *emitter) number(v float64, prec int) {
	t := formatNumber(nil, v, prec)
	if e.prevKind == 'n' && t[0] != '-' && !(t[0] == '.' && e.prevOpen) {
		e.b = append(e.b, ' ')
	}
	e.b = append(e.b, t...)
	e.prevKind = 'n'
	e.prevOpen = bytes.IndexByte(t, '.') >= 0 || bytes.IndexByte(t, 'e') >= 0
}
