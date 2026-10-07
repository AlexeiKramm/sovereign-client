package iface

import (
	"reflect"
	"testing"
)

func TestUTF16(t *testing.T) {
	cases := []struct {
		in   string
		want []uint16
	}{
		{"", nil},
		{"abc", []uint16{0x61, 0x62, 0x63}},
		{"héllo", []uint16{0x68, 0xE9, 0x6C, 0x6C, 0x6F}},
		// U+1F600 GRINNING FACE is a surrogate pair.
		{"a😀b", []uint16{0x61, 0xD83D, 0xDE00, 0x62}},
	}
	for _, c := range cases {
		got := UTF16(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("UTF16(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
