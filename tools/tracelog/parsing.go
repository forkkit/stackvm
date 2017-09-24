package main

import (
	"bytes"
	"strconv"
)

func scanKVs(s string, each func(k, v string)) {
	ks, ke, vs, ve := 0, 0, 0, 0
	bc, cc, pc := 0, 0, 0

seekKey:
	for ; ks < len(s); ks++ {
		switch s[ks] {
		case ' ', '\t', '\n':
		default:
			goto scanKey
		}
	}
	return

scanKey:
	for ke = ks; ke < len(s); ke++ {
		switch s[ke] {
		case ' ', '\t', '\n':
			ks = ke
			goto seekKey
		case '=':
			goto seekVal
		}
	}
	return

seekVal:
	vs = ke + 1
	ve = vs

scanVal:
	for ; ve < len(s); ve++ {
		switch s[ve] {
		case '"':
			ve++
			goto scanDQ
		case '\'':
			ve++
			goto scanSQ

		case '[':
			bc++
		case ']':
			if bc > 0 {
				bc--
			}

		case '{':
			cc++
		case '}':
			if cc > 0 {
				cc--
			}

		case '(':
			pc++
		case ')':
			if pc > 0 {
				pc--
			}

		case ' ', '\t', '\n':
			if bc+cc+pc <= 0 {
				goto emit
			}
		}
	}
	goto emit

scanDQ:
	for ; ve < len(s); ve++ {
		switch s[ve] {
		case '\\':
			ve++
		case '"':
			ve++
			goto scanVal
		}
	}
	goto emit

scanSQ:
	for ; ve < len(s); ve++ {
		switch s[ve] {
		case '\\':
			ve++
		case '\'':
			ve++
			goto scanVal
		}
	}
	goto emit

emit:
	each(s[ks:ke], s[vs:ve])
	ks = ve + 1
	if ks < len(s) {
		goto scanKey
	}
}

func scanVs(s string, each func(v string)) {
	var buf bytes.Buffer
	vs, ve := 0, 0
	bc, cc, pc := 0, 0, 0

seekVal:
	for ; vs < len(s); vs++ {
		switch s[vs] {
		case ' ', '\t', '\n':
		default:
			goto scanVal
		}
	}
	return

scanVal:
	for ve = vs; ve < len(s); ve++ {
		switch s[ve] {
		case '"':
			vs = ve + 1
			goto scanDQ
		case '\'':
			vs = ve + 1
			goto scanSQ

		case '[':
			bc++
		case ']':
			if bc > 0 {
				bc--
			}

		case '{':
			cc++
		case '}':
			if cc > 0 {
				cc--
			}

		case '(':
			pc++
		case ')':
			if pc > 0 {
				pc--
			}

		case ' ', '\t', '\n':
			if bc+cc+pc <= 0 {
				goto emit
			}
		}
	}
	goto emit

scanDQ:
	buf.Reset()
	for ve = vs; ve < len(s); ve++ {
		switch c := s[ve]; c {
		case '\\':
			if ve++; ve < len(s) {
				buf.WriteByte(s[ve])
			} else {
				buf.WriteByte(c)
			}
		case '"':
			each(buf.String())
			vs = ve + 2
			if vs < len(s) {
				goto seekVal
			}
			return
		default:
			buf.WriteByte(c)
		}
	}
	vs--
	goto emit

scanSQ:
	buf.Reset()
	for ve = vs; ve < len(s); ve++ {
		switch c := s[ve]; c {
		case '\\':
			if ve++; ve < len(s) {
				buf.WriteByte(s[ve])
			} else {
				buf.WriteByte(c)
			}
		case '\'':
			each(buf.String())
			vs = ve + 2
			if vs < len(s) {
				goto seekVal
			}
			return
		default:
			buf.WriteByte(c)
		}
	}
	vs--
	goto emit

emit:
	each(s[vs:ve])
	vs = ve + 1
	if vs < len(s) {
		goto seekVal
	}
}

func parseValue(s string) interface{} {
	if len(s) == 0 {
		return s
	}
	switch s[0] {
	case '[':
		if i, j := 1, len(s)-1; i <= j && s[j] == ']' {
			return parseSliceValues(s[i:j])
		}
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return int(n)
	}
	if b, err := strconv.ParseBool(s); err == nil {
		return b
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

func parseSliceValues(s string) []interface{} {
	vs := []interface{}{}
	scanVs(s, func(s string) {
		vs = append(vs, parseValue(s))
	})
	return vs
}
