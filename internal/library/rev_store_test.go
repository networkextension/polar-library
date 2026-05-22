package library

import "testing"

func TestHexToBytesValid(t *testing.T) {
	cases := []struct {
		in   string
		want []byte
	}{
		{"deadbeef", []byte{0xde, 0xad, 0xbe, 0xef}},
		{"DEADBEEF", []byte{0xde, 0xad, 0xbe, 0xef}},
		{"de ad be ef", []byte{0xde, 0xad, 0xbe, 0xef}},
		{"de:ad:be:ef", []byte{0xde, 0xad, 0xbe, 0xef}},
		{"00", []byte{0x00}},
	}
	for _, tt := range cases {
		got, err := hexToBytes(tt.in)
		if err != nil {
			t.Errorf("%q: unexpected err %v", tt.in, err)
			continue
		}
		if len(got) != len(tt.want) {
			t.Errorf("%q: len %d, want %d", tt.in, len(got), len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("%q: byte %d = %02x, want %02x", tt.in, i, got[i], tt.want[i])
			}
		}
	}
}

func TestHexToBytesInvalid(t *testing.T) {
	for _, in := range []string{"abc", "zz", "xx", "0x", "1g"} {
		if _, err := hexToBytes(in); err == nil {
			t.Errorf("%q: expected error", in)
		}
	}
}

func TestBytesToHexRoundtrip(t *testing.T) {
	orig := []byte{0x00, 0xff, 0xa5, 0x5a, 0x10, 0x20}
	hex := bytesToHex(orig)
	back, err := hexToBytes(hex)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(back) != len(orig) {
		t.Fatalf("roundtrip len %d != %d", len(back), len(orig))
	}
	for i := range back {
		if back[i] != orig[i] {
			t.Errorf("byte %d: got %02x want %02x", i, back[i], orig[i])
		}
	}
}

func TestParseFlexibleInt64(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"42", 42},
		{"0", 0},
		{"0x80001234", 0x80001234},
		{"0X80001234", 0x80001234},
		{"-7", -7},
	}
	for _, tt := range cases {
		got, err := parseFlexibleInt64(tt.in)
		if err != nil {
			t.Errorf("%q: err %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%q: got %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestIntSliceToInt64(t *testing.T) {
	if r := intSliceToInt64(nil); r != nil {
		t.Errorf("nil should stay nil; got %v", r)
	}
	if r := intSliceToInt64([]int{}); r != nil {
		t.Errorf("empty should be nil; got %v", r)
	}
	r := intSliceToInt64([]int{0x8101, 0x8103, 0x8112})
	if len(r) != 3 || r[0] != 0x8101 || r[2] != 0x8112 {
		t.Errorf("unexpected: %v", r)
	}
}
