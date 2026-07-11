package api

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
)

// Sniffed content type → stored extension. Anything else is rejected — this
// endpoint exists only for event cover images.
var uploadExts = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// handleUpload accepts a multipart image under field "file" and stores it in
// cfg.UploadDir under a random name. Responds 201 with {"data":{"url":
// "/uploads/<name>"}} — a relative path, so the client prefixes its own API
// base and the URL stays valid behind any reverse-proxy prefix.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	// Cover images only — cap the whole request well below anything abusive.
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

	f, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "multipart field 'file' is required")
		return
	}
	defer f.Close()

	// Type is decided by sniffing content, never by the client's filename.
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	ext, ok := uploadExts[http.DetectContentType(head)]
	if !ok {
		writeErr(w, 415, "only jpeg, png, webp or gif images are accepted")
		return
	}

	if err := os.MkdirAll(s.cfg.UploadDir, 0o755); err != nil {
		writeErr(w, 500, "upload dir: "+err.Error())
		return
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	name := hex.EncodeToString(buf) + ext

	dst, err := os.Create(filepath.Join(s.cfg.UploadDir, name))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer dst.Close()
	if _, err := dst.Write(head); err == nil {
		_, err = io.Copy(dst, f)
	}
	if err != nil {
		_ = os.Remove(dst.Name())
		writeErr(w, 500, err.Error())
		return
	}

	writeJSON(w, 201, envelope{Data: map[string]string{"url": "/uploads/" + name}})
}

// handleUploadServe returns a stored upload. The name is reduced to its base
// so path traversal cannot escape the uploads dir.
func (s *Server) handleUploadServe(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(chi.URLParam(r, "name"))
	if name == "." || name == "/" {
		writeErr(w, 400, "bad name")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, filepath.Join(s.cfg.UploadDir, name))
}
