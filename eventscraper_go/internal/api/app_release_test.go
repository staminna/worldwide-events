package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorgenunes/eventscraper/internal/config"
)

func TestAppReleasePage(t *testing.T) {
	dir := t.TempDir()
	st := newTestStore(t)
	s := NewServer(config.Config{AllowedOrigin: "*", UploadDir: dir}, st, nil, nil, nil)
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	get := func(path string) (*http.Response, string) {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		var sb strings.Builder
		_, _ = io.Copy(&sb, resp.Body)
		return resp, sb.String()
	}

	// No APK uploaded: page says so, download 404s.
	resp, body := get("/app")
	if resp.StatusCode != 200 || !strings.Contains(body, "isn't uploaded yet") {
		t.Fatalf("empty page: status=%d body has notice=%v", resp.StatusCode, strings.Contains(body, "isn't uploaded yet"))
	}
	resp, _ = get("/app/android")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing apk status = %d, want 404", resp.StatusCode)
	}

	// Drop a build in the data dir: page links it, download serves it with
	// the Android MIME.
	if err := os.WriteFile(filepath.Join(dir, apkName), []byte("fake-apk-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, body = get("/app")
	if resp.StatusCode != 200 || !strings.Contains(body, `href="app/android"`) {
		t.Fatalf("page after upload: status=%d body=%q", resp.StatusCode, body[:min(200, len(body))])
	}
	resp, body = get("/app/android")
	if resp.StatusCode != 200 ||
		resp.Header.Get("Content-Type") != "application/vnd.android.package-archive" ||
		body != "fake-apk-bytes" {
		t.Fatalf("apk download: status=%d type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}
