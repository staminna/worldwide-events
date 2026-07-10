package api

import (
	"bufio"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// handleImg is a CORS-friendly image proxy. Upstream image hosts often:
//   - omit Access-Control-Allow-Origin (breaks Flutter Web)
//   - gate on the Referer header (returns 403 to bots)
//   - send empty/wrong Content-Type (we sniff)
//   - serve over HTTP only (we upgrade to HTTPS when the upstream supports it)
//
// We fetch with a desktop UA + a host-appropriate Referer, sniff the type
// from the bytes if needed, and stream back with CORS + a 24h cache.
func (s *Server) handleImg(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("u")
	if raw == "" {
		writeErr(w, http.StatusBadRequest, "missing u parameter")
		return
	}
	target, err := url.Parse(raw)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") {
		writeErr(w, http.StatusBadRequest, "invalid url")
		return
	}
	// Try HTTPS first for hosts that we know support it (avoids mixed content
	// when the frontend is served over HTTPS).
	if target.Scheme == "http" {
		https := *target
		https.Scheme = "https"
		target = &https
	}

	body, ct, status := fetchImage(r, target.String())
	if status != 0 {
		w.WriteHeader(status)
		return
	}
	defer body.Close()

	// Sniff if upstream didn't give us a usable Content-Type.
	br := bufio.NewReader(body)
	if !strings.HasPrefix(ct, "image/") {
		head, _ := br.Peek(512)
		sniffed := http.DetectContentType(head)
		if !strings.HasPrefix(sniffed, "image/") {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		ct = sniffed
	}

	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, io.LimitReader(br, 20<<20))
}

// fetchImage performs the upstream GET, returning (body, contentType, 0) on
// success, or (nil, "", statusCode) on failure where statusCode is what the
// proxy should return.
func fetchImage(r *http.Request, urlStr string) (io.ReadCloser, string, int) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, "", http.StatusBadGateway
	}
	u, _ := url.Parse(urlStr)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; eventscraper-img/1.0)")
	req.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,image/*;q=0.8,*/*;q=0.1")
	req.Header.Set("Referer", refererFor(u.Host))

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", http.StatusBadGateway
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		// Upstream genuinely missing — propagate 404 so the browser caches it
		// and stops re-requesting the proxy.
		return nil, "", http.StatusNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, "", http.StatusBadGateway
	}
	return resp.Body, resp.Header.Get("Content-Type"), 0
}

// refererFor picks a plausible Referer for hotlink-checking CDNs.
func refererFor(host string) string {
	h := strings.ToLower(host)
	switch {
	case strings.Contains(h, "evbuc.com") || strings.Contains(h, "eventbrite"):
		return "https://www.eventbrite.com/"
	case strings.Contains(h, "songkick") || strings.Contains(h, "sk-static"):
		return "https://www.songkick.com/"
	case strings.Contains(h, "lumacdn.com") || strings.Contains(h, "lu.ma"):
		return "https://lu.ma/"
	case strings.Contains(h, "ticketm.net") || strings.Contains(h, "ticketmaster"):
		return "https://www.ticketmaster.com/"
	}
	return "https://" + host + "/"
}
