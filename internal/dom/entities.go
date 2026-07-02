package dom

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

// decodeAttrValue strips the surrounding quotes (when present) and resolves
// the five XML entities, numeric character references, and entities declared
// in the document's DTD internal subset (Illustrator binds its namespaces
// through those). A reference it cannot resolve marks the value opaque: kept
// literally and never rewritten by any pass.
func decodeAttrValue(v []byte, custom map[string]string) (value string, opaque bool) {
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
		if end < 0 || end > 64 {
			return string(v), true
		}
		name := string(v[i+1 : i+end])
		if r, ok := resolveEntity(name); ok {
			b.WriteRune(r)
		} else if s, ok := custom[name]; ok {
			b.WriteString(s)
		} else {
			return string(v), true
		}
		i += end + 1
	}
	return b.String(), false
}

var entityDeclPattern = regexp.MustCompile(`<!ENTITY\s+([\w.-]+)\s+"([^"%&]*)"\s*>`)

// parseInternalSubset extracts simple <!ENTITY name "value"> declarations
// from a DOCTYPE's internal subset. Parameter entities and references inside
// replacement text are beyond what this resolves; values using them stay
// opaque through decodeAttrValue.
func parseInternalSubset(doctype []byte) map[string]string {
	i := bytes.IndexByte(doctype, '[')
	if i < 0 {
		return nil
	}
	out := map[string]string{}
	for _, m := range entityDeclPattern.FindAllSubmatch(doctype[i:], -1) {
		out[string(m[1])] = string(m[2])
	}
	return out
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
