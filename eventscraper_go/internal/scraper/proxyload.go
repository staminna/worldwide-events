package scraper

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"
)

// LoadProxies resolves proxies from the first non-empty source, in order:
// inline value, local file, remote URL (e.g. a Webshare tokenized download
// URL). Returns the parsed URLs (possibly empty ⇒ direct connections).
func LoadProxies(inline, path, remoteURL string) []*url.URL {
	if inline != "" {
		return ParseProxies(inline)
	}
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("proxy list file unreadable", "path", path, "err", err)
		} else {
			return ParseProxies(string(b))
		}
	}
	if remoteURL != "" {
		return fetchProxyList(remoteURL)
	}
	return nil
}

func fetchProxyList(remoteURL string) []*url.URL {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, remoteURL, nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("proxy list fetch failed", "err", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("proxy list fetch status", "status", resp.StatusCode)
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return ParseProxies(string(b))
}

// AutoReloadProxies periodically re-fetches the proxy list from remoteURL and
// swaps it into the pool in place. No-op when remoteURL is empty. Blocks until
// ctx is done, so run it in a goroutine.
func AutoReloadProxies(ctx context.Context, pool *ProxyPool, remoteURL string, every time.Duration) {
	if remoteURL == "" || every <= 0 {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if us := fetchProxyList(remoteURL); len(us) > 0 {
				pool.Set(us)
				slog.Info("proxy list reloaded", "count", len(us))
			}
		}
	}
}
