package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_scanKVs(t *testing.T) {
	for _, tc := range []struct {
		in  string
		out []string
	}{
		{"", nil},
		{"foo", nil},
		{"foo=", []string{"foo", ""}},
		{"foo=bar", []string{"foo", "bar"}},
		{" foo=bar", []string{"foo", "bar"}},
		{" foo=bar ", []string{"foo", "bar"}},
		{"mid=1(2:3)", []string{"mid", "1(2:3)"}},
		{"values=[3 1 4]", []string{"values", "[3 1 4]"}},
		{"mid=1(2:3) values=[3 1 4]",
			[]string{"mid", "1(2:3)", "values", "[3 1 4]"}},
		{"mid=1(2:3) lol values=[3 1 4]",
			[]string{"mid", "1(2:3)", "values", "[3 1 4]"}},
		{"foo='bar'", []string{"foo", "'bar'"}},
		{`foo="bar"`, []string{"foo", `"bar"`}},
		{`garbage=[3 {1 4] ('ab"c'} "1'00")`, []string{"garbage", `[3 {1 4] ('ab"c'} "1'00")`}},
		{`err="lol"`, []string{"err", `"lol"`}},
		{`err="lol wut"`, []string{"err", `"lol wut"`}},
		{`err='lol'`, []string{"err", `'lol'`}},
		{`err='lol wut'`, []string{"err", `'lol wut'`}},
	} {
		t.Run(tc.in, func(t *testing.T) {
			var ss []string
			scanKVs(tc.in, func(k, v string) {
				ss = append(ss, k, v)
			})
			assert.Equal(t, tc.out, ss)

		})
	}
}

func Test_parseInts(t *testing.T) {
	for _, tc := range []struct {
		s   string
		ns  []int
		err string
	}{
		{s: "", err: "expected ["},
		{s: "[", err: "unexpected end-of-string"},
		{s: "[3", err: "unexpected end-of-string"},
		{s: "[3,2]", err: "unexpected ','"},
		{s: "[]", ns: []int{}},
		{s: "[42]", ns: []int{42}},
		{s: "[3 1 4]", ns: []int{3, 1, 4}},
		{s: "[0]", ns: []int{0}},
		{s: "[10]", ns: []int{10}},
		{s: "[104]", ns: []int{104}},
	} {
		t.Run(tc.s, func(t *testing.T) {
			ns, err := parseInts(tc.s)
			if tc.err != "" {
				assert.EqualError(t, err, tc.err, "expected error")
			} else if assert.NoError(t, err, "unexpected error") {
				assert.Equal(t, tc.ns, ns, "expected ints")
			}
		})
	}
}
