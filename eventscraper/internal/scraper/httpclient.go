package scraper

import (
	"math/rand"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// StealthClient is the shared HTTP client for the net/http scrapers (and image
// enrichment). Every request gets a rotating browser User-Agent + realistic
// headers, is routed through the next proxy in the pool (or direct when the
// pool is empty), and is retried with exponential backoff on transient blocks
// (403/408/429/5xx) or transport errors — rotating to a fresh proxy each
// attempt and cooling down proxies that fail to connect.
type StealthClient struct {
	pool       *ProxyPool
	timeout    time.Duration
	maxRetries int
	baseDelay  time.Duration

	sleep func(time.Duration) // injectable; time.Sleep in prod, no-op in tests

	rmu sync.Mutex
	rnd *rand.Rand

	cmu     sync.Mutex
	clients map[string]*http.Client // one keep-alive client per proxy (""=direct)
}

// StealthConfig configures a StealthClient. A nil Pool (or one with no
// proxies) means direct connections.
type StealthConfig struct {
	Pool       *ProxyPool
	Timeout    time.Duration
	MaxRetries int
	BaseDelay  time.Duration
	Sleep      func(time.Duration) // tests pass a no-op
	Rand       *rand.Rand
}

// NewStealthClient builds a client from cfg, applying sensible defaults.
func NewStealthClient(cfg StealthConfig) *StealthClient {
	if cfg.Pool == nil {
		cfg.Pool = NewProxyPool(nil)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = 500 * time.Millisecond
	}
	if cfg.Sleep == nil {
		cfg.Sleep = time.Sleep
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return &StealthClient{
		pool:       cfg.Pool,
		timeout:    cfg.Timeout,
		maxRetries: cfg.MaxRetries,
		baseDelay:  cfg.BaseDelay,
		sleep:      cfg.Sleep,
		rnd:        cfg.Rand,
		clients:    map[string]*http.Client{},
	}
}

// Do sends req with stealth headers, proxy rotation and retry/backoff. The
// caller keeps ownership of the returned response body as usual. Semantic
// headers already set on req (Accept, Accept-Language) are preserved; the
// User-Agent is always replaced.
func (c *StealthClient) Do(req *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		proxy := c.pool.Next()
		attemptReq := req.Clone(req.Context())
		c.rmu.Lock()
		ua := RandomUA(c.rnd)
		c.rmu.Unlock()
		applyBrowserHeaders(attemptReq, ua)

		resp, err := c.clientFor(proxy).Do(attemptReq)
		if err != nil {
			c.pool.MarkBad(proxy) // connection-level failure: cool this proxy
			if attempt >= c.maxRetries {
				return nil, err
			}
			c.backoff(attempt)
			continue
		}
		if isRetryableStatus(resp.StatusCode) && attempt < c.maxRetries {
			resp.Body.Close()
			if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
				c.pool.MarkBad(proxy)
			}
			c.backoff(attempt)
			continue
		}
		return resp, nil
	}
}

func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusForbidden, // 403
		http.StatusRequestTimeout,   // 408
		http.StatusTooManyRequests,  // 429
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// backoff sleeps base*2^attempt plus up to one base of jitter.
func (c *StealthClient) backoff(attempt int) {
	d := c.baseDelay << attempt
	c.rmu.Lock()
	jitter := time.Duration(c.rnd.Int63n(int64(c.baseDelay) + 1))
	c.rmu.Unlock()
	c.sleep(d + jitter)
}

// clientFor returns a keep-alive *http.Client bound to the given proxy (nil ⇒
// direct), building and caching one per proxy so connections are reused.
func (c *StealthClient) clientFor(proxy *url.URL) *http.Client {
	key := ""
	if proxy != nil {
		key = proxy.String()
	}
	c.cmu.Lock()
	defer c.cmu.Unlock()
	if cl, ok := c.clients[key]; ok {
		return cl
	}
	tr := &http.Transport{
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	if proxy != nil {
		p := proxy // capture
		tr.Proxy = func(*http.Request) (*url.URL, error) { return p, nil }
	}
	cl := &http.Client{Timeout: c.timeout, Transport: tr}
	c.clients[key] = cl
	return cl
}
