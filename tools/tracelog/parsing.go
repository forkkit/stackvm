package main

import (
	"errors"
	"fmt"
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

func parseInts(s string) ([]int, error) {
	i := 0
	if len(s) < 1 || s[i] != '[' {
		return nil, errors.New("expected [")
	}
	i++
	ns := []int{}
	var n int
	for j := i; j < len(s); j++ {
		switch c := s[j]; c {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			n = 10*n + int(c-'0')
		case ' ':
			ns = append(ns, n)
			n = 0
			i = j
		case ']':
			if j > i {
				ns = append(ns, n)
			}
			return ns, nil
		default:
			return nil, fmt.Errorf("unexpected %q", c)
		}
	}
	return nil, errors.New("unexpected end-of-string")
}
