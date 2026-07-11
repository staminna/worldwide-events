package scraper

import (
	"testing"
	"time"
)

func TestParseProxiesFormats(t *testing.T) {
	raw := "1.1.1.1:8000:user:pass, http://u:p@2.2.2.2:9000\n3.3.3.3:7000\nsocks5://4.4.4.4:1080"
	got := ParseProxies(raw)
	if len(got) != 4 {
		t.Fatalf("parsed %d proxies, want 4: %v", len(got), got)
	}
	// host:port:user:pass → http with creds
	if got[0].Scheme != "http" || got[0].Host != "1.1.1.1:8000" ||
		got[0].User.Username() != "user" {
		t.Errorf("token 0 = %+v", got[0])
	}
	// full URL preserved
	if got[1].Host != "2.2.2.2:9000" {
		t.Errorf("token 1 host = %q", got[1].Host)
	}
	// bare host:port
	if got[2].Scheme != "http" || got[2].Host != "3.3.3.3:7000" || got[2].User != nil {
		t.Errorf("token 2 = %+v", got[2])
	}
	if got[3].Scheme != "socks5" {
		t.Errorf("token 3 scheme = %q", got[3].Scheme)
	}
}

func TestProxyPoolRoundRobin(t *testing.T) {
	p := NewProxyPool(ParseProxies("a:1,b:2,c:3"))
	var seen []string
	for i := 0; i < 6; i++ {
		seen = append(seen, p.Next().Host)
	}
	want := []string{"a:1", "b:2", "c:3", "a:1", "b:2", "c:3"}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("Next()[%d] = %s, want %s (round-robin)", i, seen[i], want[i])
		}
	}
}

func TestProxyPoolEmptyIsDirect(t *testing.T) {
	p := NewProxyPool(nil)
	if p.Len() != 0 {
		t.Fatalf("empty pool Len = %d", p.Len())
	}
	if p.Next() != nil {
		t.Errorf("Next() on empty pool should be nil (direct)")
	}
	if len(p.URLStrings()) != 0 {
		t.Errorf("URLStrings on empty pool should be empty")
	}
}

func TestProxyPoolMarkBadSkipsUntilCooldown(t *testing.T) {
	fake := time.Unix(1_700_000_000, 0)
	p := NewProxyPool(ParseProxies("a:1,b:2"))
	p.now = func() time.Time { return fake }

	bad := p.Next() // a:1
	p.MarkBad(bad)
	// b:2 is healthy; a:1 is cooling down, so the next few calls skip it.
	for i := 0; i < 3; i++ {
		if got := p.Next(); got == nil || got.Host != "b:2" {
			t.Fatalf("call %d returned %v, want b:2 while a:1 cools down", i, got)
		}
	}
	// After the cooldown elapses, a:1 is eligible again.
	fake = fake.Add(defaultProxyCooldown + time.Second)
	hosts := map[string]bool{}
	for i := 0; i < 2; i++ {
		hosts[p.Next().Host] = true
	}
	if !hosts["a:1"] {
		t.Errorf("a:1 never returned after cooldown; saw %v", hosts)
	}
}

func TestProxyPoolAllBadFallsBackToDirect(t *testing.T) {
	fake := time.Unix(1_700_000_000, 0)
	p := NewProxyPool(ParseProxies("a:1,b:2"))
	p.now = func() time.Time { return fake }
	p.MarkBad(p.entries[0].u)
	p.MarkBad(p.entries[1].u)
	if p.Next() != nil {
		t.Errorf("all proxies cooling down should yield nil (direct)")
	}
}
