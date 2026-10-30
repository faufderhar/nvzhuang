package security

import (
	"net/netip"
	"testing"
)

func TestParseTrustedProxies(t *testing.T) {
	got, err := ParseTrustedProxies([]string{"127.0.0.1", " 10.0.0.0/8 ", "::1", ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || !got[0].Contains(netip.MustParseAddr("127.0.0.1")) || !got[1].Contains(netip.MustParseAddr("10.2.3.4")) {
		t.Fatalf("unexpected prefixes: %v", got)
	}
	for _, bad := range []string{"not-an-ip", "10.0.0.0/33"} {
		if _, err := ParseTrustedProxies([]string{bad}); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}
