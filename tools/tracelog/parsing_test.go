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

func Test_parseValue(t *testing.T) {
	for _, tc := range []struct {
		s string
		v interface{}
	}{
		{"true", true},
		{"false", false},
		{"0", 0},
		{"1", 1},
		{"42", 42},
		{"99.9", 99.9},
		{"", ""},
		{"[", "["},
		{"[3", "[3"},
		{"[3,2]", []interface{}{"3,2"}},
		{"[]", []interface{}{}},
		{"[42]", []interface{}{42}},
		{"[3 1 4]", []interface{}{3, 1, 4}},
		{"[0]", []interface{}{0}},
		{"[10]", []interface{}{10}},
		{"[104]", []interface{}{104}},
		{`["foo" 'bar']`, []interface{}{"foo", "bar"}},
		{`["foo" 42 'bar' false]`, []interface{}{"foo", 42, "bar", false}},
		{`["lol \"wut\"" 'how \'now\'']`, []interface{}{`lol "wut"`, `how 'now'`}},
		{`"foo`, "\"foo"},
		{`'foo`, "'foo"},
	} {
		t.Run(tc.s, func(t *testing.T) {
			assert.Equal(t, tc.v, parseValue(tc.s), "expected value")
		})
	}
}
