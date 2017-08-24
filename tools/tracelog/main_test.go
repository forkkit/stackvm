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
