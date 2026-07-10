package scraper

import (
	"net/url"
	"strings"
	"sync"
	"time"
)

// defaultProxyCooldown is how long a proxy is skipped after it fails, before
// the pool tries it again.
const defaultProxyCooldown = 2 * time.Minute

type proxyEntry struct {
	u        *url.URL
	badUntil time.Time
}

// ProxyPool is a round-robin pool of upstream proxies with per-proxy failure
// cool-down. It is safe for concurrent use. An empty pool is valid and means
// "connect directly" — every accessor degrades to the no-proxy case, so the
// rest of the engine needs no special-casing when no proxies are configured.
type ProxyPool struct {
	mu       sync.Mutex
	entries  []*proxyEntry
	idx      int
	cooldown time.Duration
	now      func() time.Time // injectable for tests
}

// NewProxyPool builds a pool from parsed proxy URLs (may be empty).
func NewProxyPool(urls []*url.URL) *ProxyPool {
	p := &ProxyPool{cooldown: defaultProxyCooldown, now: time.Now}
	p.Set(urls)
	return p
}

// Set replaces the pool's proxies (used by the periodic reload). Preserves the
// round-robin cursor position modulo the new length so a reload doesn't reset
// rotation to the top.
func (p *ProxyPool) Set(urls []*url.URL) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries = make([]*proxyEntry, 0, len(urls))
	for _, u := range urls {
		if u != nil {
			p.entries = append(p.entries, &proxyEntry{u: u})
		}
	}
	if len(p.entries) > 0 {
		p.idx %= len(p.entries)
	} else {
		p.idx = 0
	}
}

// Len reports the number of proxies in the pool.
func (p *ProxyPool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}

// Next returns the next healthy proxy in round-robin order, or nil when the
// pool is empty or every proxy is currently cooling down (⇒ connect directly).
func (p *ProxyPool) Next() *url.URL {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := len(p.entries)
	if n == 0 {
		return nil
	}
	now := p.now()
	for i := 0; i < n; i++ {
		e := p.entries[p.idx%n]
		p.idx++
		if now.After(e.badUntil) {
			return e.u
		}
	}
	return nil
}

// MarkBad puts a proxy on cool-down after a failed request.
func (p *ProxyPool) MarkBad(u *url.URL) {
	if u == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.entries {
		if e.u.String() == u.String() {
			e.badUntil = p.now().Add(p.cooldown)
			return
		}
	}
}

// URLStrings returns every proxy URL as a string, for consumers (e.g. colly's
// RoundRobinProxySwitcher) that want a static list. Cool-down is ignored here.
func (p *ProxyPool) URLStrings() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.entries))
	for _, e := range p.entries {
		out = append(out, e.u.String())
	}
	return out
}

// ParseProxies parses a whitespace/comma/newline-separated list of proxies.
// Each token is either a full URL (`http://user:pass@host:port`,
// `socks5://host:port`) or Webshare's download format `host:port:user:pass`
// (or a bare `host:port`). Unparseable tokens are skipped.
func ParseProxies(raw string) []*url.URL {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t'
	})
	out := make([]*url.URL, 0, len(fields))
	for _, tok := range fields {
		if u := parseProxyToken(tok); u != nil {
			out = append(out, u)
		}
	}
	return out
}

func parseProxyToken(tok string) *url.URL {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return nil
	}
	if strings.Contains(tok, "://") {
		if u, err := url.Parse(tok); err == nil && u.Host != "" {
			return u
		}
		return nil
	}
	// Colon-delimited forms. Webshare's list download is host:port:user:pass.
	parts := strings.Split(tok, ":")
	switch len(parts) {
	case 2: // host:port
		return &url.URL{Scheme: "http", Host: parts[0] + ":" + parts[1]}
	case 4: // host:port:user:pass
		return &url.URL{
			Scheme: "http",
			User:   url.UserPassword(parts[2], parts[3]),
			Host:   parts[0] + ":" + parts[1],
		}
	}
	return nil
}
