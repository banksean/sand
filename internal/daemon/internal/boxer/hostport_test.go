package boxer

import (
	"strings"
	"testing"
)

func TestBuildHostPortIptablesScript(t *testing.T) {
	script := buildHostPortIptablesScript("192.168.64.1", []int{3845, 5173})
	wantSubstrings := []string{
		"route_localnet=1",
		"iptables -t nat -A OUTPUT -p tcp -d 127.0.0.1 --dport 3845 -j DNAT --to-destination 192.168.64.1:3845",
		"iptables -t nat -A OUTPUT -p tcp -d 127.0.0.1 --dport 5173 -j DNAT --to-destination 192.168.64.1:5173",
		"iptables -t nat -A POSTROUTING -p tcp -d 192.168.64.1 --dport 3845 -j MASQUERADE",
		"iptables -t nat -A POSTROUTING -p tcp -d 192.168.64.1 --dport 5173 -j MASQUERADE",
		"iptables -t nat -C OUTPUT", // idempotency check
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(script, s) {
			t.Errorf("script missing %q\nfull:\n%s", s, script)
		}
	}
}

func TestHostPortsRoundTrip(t *testing.T) {
	ns := hostPortsToNullString([]int{1, 22, 3845})
	if !ns.Valid || ns.String != "1,22,3845" {
		t.Fatalf("hostPortsToNullString = %+v", ns)
	}
	got := hostPortsFromNullString(ns)
	if len(got) != 3 || got[0] != 1 || got[1] != 22 || got[2] != 3845 {
		t.Fatalf("round trip = %v", got)
	}
	if hostPortsToNullString(nil).Valid {
		t.Fatalf("empty should be NULL")
	}
	if hostPortsFromNullString(hostPortsToNullString(nil)) != nil {
		t.Fatalf("empty round trip should be nil")
	}
}
