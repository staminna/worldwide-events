package scraper

import (
	"math/rand"
	"net/http"
)

// userAgents is a small pool of current, real desktop browser UA strings
// (Chrome, Firefox, Safari, Edge on Windows/macOS). Rotating a plausible UA
// per request is the cheapest, highest-leverage anti-fingerprinting measure.
// Keep these reasonably fresh — a UA advertising a years-old browser is itself
// a bot tell.
var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.6 Safari/605.1.15",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Safari/605.1.15",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:132.0) Gecko/20100101 Firefox/132.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 14.6; rv:133.0) Gecko/20100101 Firefox/133.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0.0.0",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64; rv:133.0) Gecko/20100101 Firefox/133.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15",
}

// UserAgents returns the pool, for callers (e.g. colly) that want to seed their
// own rotation from the same list.
func UserAgents() []string { return userAgents }

// RandomUA returns a random user-agent from the pool.
func RandomUA(rnd *rand.Rand) string {
	if rnd != nil {
		return userAgents[rnd.Intn(len(userAgents))]
	}
	return userAgents[rand.Intn(len(userAgents))]
}

// applyBrowserHeaders sets a realistic set of browser request headers. The
// User-Agent is always overwritten (that's the whole point); the rest are only
// filled when the caller hasn't set them, so semantic headers a scraper needs
// (e.g. Accept: application/json for a JSON API, a specific Accept-Language)
// are preserved.
func applyBrowserHeaders(req *http.Request, ua string) {
	req.Header.Set("User-Agent", ua)
	setIfAbsent := func(k, v string) {
		if req.Header.Get(k) == "" {
			req.Header.Set(k, v)
		}
	}
	setIfAbsent("Accept",
		"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	setIfAbsent("Accept-Language", "en-US,en;q=0.9")
	// Deliberately NOT setting Accept-Encoding: Go's http.Transport only
	// transparently decompresses gzip when it adds the header itself. Setting
	// it here would hand callers raw compressed bytes and break parsing.
	setIfAbsent("Upgrade-Insecure-Requests", "1")
	setIfAbsent("Sec-Fetch-Dest", "document")
	setIfAbsent("Sec-Fetch-Mode", "navigate")
	setIfAbsent("Sec-Fetch-Site", "none")
	setIfAbsent("Sec-Fetch-User", "?1")
	setIfAbsent("DNT", "1")
}
