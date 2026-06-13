package x

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestPad64(t *testing.T) {
	cases := map[string]int{"": 0, "AA": 4, "AAA": 4, "AAAA": 4, "AAAAAA": 8}
	for in, wantLen := range cases {
		got := pad64(in)
		if len(got) != wantLen {
			t.Errorf("pad64(%q) length = %d, want %d", in, len(got), wantLen)
		}
		if _, err := base64.StdEncoding.DecodeString(got); err != nil && in != "" {
			t.Errorf("pad64(%q) = %q not decodable: %v", in, got, err)
		}
	}
}

func TestGenerateTID(t *testing.T) {
	// A plausible pair: verification is valid base64, animationKey is opaque.
	pair := tidPair{
		AnimationKey: "4b3c2d1e0f9a8b7c",
		Verification: base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")),
	}
	tid, err := generateTID("GET", "/i/api/graphql/abc/UserByScreenName?variables=%7B%7D", pair)
	if err != nil {
		t.Fatal(err)
	}
	if tid == "" {
		t.Fatal("empty transaction id")
	}
	if strings.HasSuffix(tid, "=") {
		t.Errorf("transaction id %q must not carry base64 padding", tid)
	}
	// The header is a base64 string of the obfuscated key||time||hash||0x03 buffer.
	raw, err := base64.StdEncoding.DecodeString(pad64(tid))
	if err != nil {
		t.Fatalf("transaction id not base64: %v", err)
	}
	// 1 random byte + 32 key + 4 time + 16 hash + 1 marker = 54 bytes.
	if len(raw) != 1+32+4+16+1 {
		t.Errorf("decoded length = %d, want %d", len(raw), 1+32+4+16+1)
	}
}
