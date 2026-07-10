package geocode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(srvURL string) *Client {
	return &Client{
		HTTP:        &http.Client{Timeout: 2 * time.Second},
		BaseURL:     srvURL,
		MinInterval: 10 * time.Millisecond,
		MaxWait:     time.Second,
	}
}

func TestKey(t *testing.T) {
	if got := Key(38.722252, -9.139337); got != "38.72225,-9.13934" {
		t.Errorf("Key = %q", got)
	}
	if Key(38.7222501, -9.1393401) != Key(38.7222502, -9.1393404) {
		t.Error("nearby coords should share a key")
	}
}

func TestReverseRequestShapeAndCompose(t *testing.T) {
	var gotUA, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{
			"display_name": "96, Rua das Portas de Santo Antão, Santo António, Lisboa, 1150-269, Portugal",
			"address": {"house_number":"96","road":"Rua das Portas de Santo Antão","postcode":"1150-269","city":"Lisboa"}
		}`))
	}))
	defer srv.Close()

	addr, err := testClient(srv.URL).Reverse(context.Background(), 38.7169, -9.1399)
	if err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if addr != "Rua das Portas de Santo Antão 96, 1150-269 Lisboa" {
		t.Errorf("addr = %q", addr)
	}
	if !strings.Contains(gotUA, "eventscraper/1.0") {
		t.Errorf("User-Agent = %q, must identify the app (Nominatim policy)", gotUA)
	}
	for _, want := range []string{"format=jsonv2", "addressdetails=1", "zoom=18", "lat=38.7169", "lon=-9.1399"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
}

func TestReverseComposition(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "no house number",
			body: `{"address":{"road":"Rua Augusta","postcode":"1100-048","city":"Lisboa"}}`,
			want: "Rua Augusta, 1100-048 Lisboa",
		},
		{
			name: "town fallback for locality",
			body: `{"address":{"road":"Main St","house_number":"5","town":"Sintra"}}`,
			want: "Main St 5, Sintra",
		},
		{
			name: "no road falls back to trimmed display_name",
			body: `{"display_name":"Parque Eduardo VII, Avenidas Novas, Lisboa, 1070-051, Portugal, Europe","address":{"city":"Lisboa"}}`,
			want: "Parque Eduardo VII, Avenidas Novas, Lisboa, 1070-051",
		},
		{
			name: "nothing usable is a cacheable negative",
			body: `{"error":"Unable to geocode"}`,
			want: "",
		},
		{
			name: "empty response",
			body: `{}`,
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			addr, err := testClient(srv.URL).Reverse(context.Background(), 1, 2)
			if err != nil {
				t.Fatalf("Reverse: %v", err)
			}
			if addr != tc.want {
				t.Errorf("addr = %q, want %q", addr, tc.want)
			}
		})
	}
}

func TestReverseHTTPErrorIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	if _, err := testClient(srv.URL).Reverse(context.Background(), 1, 2); err == nil {
		t.Fatal("expected error on 429 — transport failures must not look like negatives")
	}
}

func TestLimiterSpacingAndBoundedQueue(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"address":{"road":"R","city":"C"}}`))
	}))
	defer srv.Close()

	c := testClient(srv.URL)
	c.MinInterval = 60 * time.Millisecond

	start := time.Now()
	for i := range 3 {
		if _, err := c.Reverse(context.Background(), 1, 2); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if elapsed := time.Since(start); elapsed < 120*time.Millisecond {
		t.Errorf("3 calls took %v, want >= 2×MinInterval (limiter not spacing)", elapsed)
	}
	if hits.Load() != 3 {
		t.Errorf("hits = %d, want 3", hits.Load())
	}

	// With a huge interval and a tiny MaxWait, the second immediate call
	// must fail fast instead of queueing.
	c2 := testClient(srv.URL)
	c2.MinInterval = time.Hour
	c2.MaxWait = 10 * time.Millisecond
	if _, err := c2.Reverse(context.Background(), 1, 2); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := c2.Reverse(context.Background(), 1, 2); err == nil {
		t.Fatal("second call should fail fast when the queue exceeds MaxWait")
	}
}
