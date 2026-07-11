package api

import (
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
)

// The app-download landing page: a public, device-to-device shareable link
// (…/app) with a direct Android APK download. The APK itself lives in the
// data volume next to the uploads (dropped there by the release process),
// so shipping a new build is a file copy — no image rebuild.

//go:embed static/app.html
var appHTML string

var appTmpl = template.Must(template.New("app").Parse(appHTML))

const apkName = "eventscraper.apk"

func (s *Server) apkPath() string {
	return filepath.Join(s.cfg.UploadDir, apkName)
}

func (s *Server) handleAppPage(w http.ResponseWriter, _ *http.Request) {
	data := struct {
		HasAPK  bool
		SizeMB  string
		Updated string
	}{}
	if st, err := os.Stat(s.apkPath()); err == nil {
		data.HasAPK = true
		data.SizeMB = fmt.Sprintf("%.0f", float64(st.Size())/(1<<20))
		data.Updated = st.ModTime().UTC().Format("2006-01-02")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = appTmpl.Execute(w, data)
}

func (s *Server) handleAppAndroid(w http.ResponseWriter, r *http.Request) {
	path := s.apkPath()
	if _, err := os.Stat(path); err != nil {
		writeErr(w, http.StatusNotFound, "no Android build uploaded yet")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", "attachment; filename=\"worldwide-events.apk\"")
	http.ServeFile(w, r, path)
}
