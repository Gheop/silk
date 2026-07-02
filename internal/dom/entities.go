package dom

import (
	"bytes"
	"strconv"
	"strings"
)

// decodeAttrValue strips the surrounding quotes (when present) and resolves
// the five XML entities plus numeric character references. A reference it
// cannot resolve (e.g. one defined in a DTD internal subset) marks the value
// opaque: kept literally and never rewritten by any pass.
func decodeAttrValue(v []byte) (value string, opaque bool) {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		v = v[1 : len(v)-1]
	}
	if !bytes.ContainsRune(v, '&') {
		return string(v), false
	}
	var b strings.Builder
	b.Grow(len(v))
	for i := 0; i < len(v); {
		if v[i] != '&' {
			b.WriteByte(v[i])
			i++
			continue
		}
		end := bytes.IndexByte(v[i:], ';')
		if end < 0 || end > 12 {
			return string(v), true
		}
		r, ok := resolveEntity(string(v[i+1 : i+end]))
		if !ok {
			return string(v), true
		}
		b.WriteRune(r)
		i += end + 1
	}
	return b.String(), false
}

func resolveEntity(name string) (rune, bool) {
	switch name {
	case "amp":
		return '&', true
	case "lt":
		return '<', true
	case "gt":
		return '>', true
	case "quot":
		return '"', true
	case "apos":
		return '\'', true
	}
	if len(name) > 1 && name[0] == '#' {
		digits, base := name[1:], 10
		if digits[0] == 'x' || digits[0] == 'X' {
			digits, base = digits[1:], 16
		}
		n, err := strconv.ParseUint(digits, base, 32)
		if err == nil && n > 0 && n <= 0x10FFFF {
			return rune(n), true
		}
	}
	return 0, false
}

// escapeAttrTo writes v escaped for a double-quoted attribute value.
func escapeAttrTo(b *bytes.Buffer, v string) {
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteByte(v[i])
		}
	}
}
