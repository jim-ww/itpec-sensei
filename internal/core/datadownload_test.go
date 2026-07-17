package core

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func buildFakeArchive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	files := map[string]string{
		"2018A_FE-AM.json":          `{"examId":"2018A_FE-AM"}`,
		"images/2018A_FE-AM/q1.png": "fake-png-bytes",
	}
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write content: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func TestDownloadAndInstall(t *testing.T) {
	archive := buildFakeArchive(t)

	assetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	}))
	defer assetSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := githubRelease{TagName: "v0.2.0"}
		rel.Assets = []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		}{{Name: dataAssetName, BrowserDownloadURL: assetSrv.URL}}
		json.NewEncoder(w).Encode(rel)
	}))
	defer apiSrv.Close()

	origAPI := dataReleaseAPI
	dataReleaseAPI = apiSrv.URL
	defer func() { dataReleaseAPI = origAPI }()

	dataDir := t.TempDir()

	if Installed(dataDir) {
		t.Fatal("expected not installed before download")
	}

	tag, assetURL, err := LatestRelease(t.Context())
	if err != nil {
		t.Fatalf("LatestRelease: %v", err)
	}
	if tag != "v0.2.0" {
		t.Fatalf("tag = %q, want v0.2.0", tag)
	}

	if err := DownloadAndInstall(t.Context(), dataDir, tag, assetURL); err != nil {
		t.Fatalf("DownloadAndInstall: %v", err)
	}

	if !Installed(dataDir) {
		t.Fatal("expected installed after download")
	}
	if v, ok := InstalledVersion(dataDir); !ok || v != "v0.2.0" {
		t.Fatalf("InstalledVersion = %q, %v", v, ok)
	}

	if _, err := os.Stat(filepath.Join(dataDir, "questions", "2018A_FE-AM.json")); err != nil {
		t.Fatalf("expected extracted json file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "questions", "images", "2018A_FE-AM", "q1.png")); err != nil {
		t.Fatalf("expected extracted image file: %v", err)
	}

	current, latest, hasUpdate, err := CheckUpdate(t.Context(), dataDir)
	if err != nil {
		t.Fatalf("CheckUpdate: %v", err)
	}
	if current != "v0.2.0" || latest != "v0.2.0" || hasUpdate {
		t.Fatalf("CheckUpdate = %q, %q, %v, want v0.2.0, v0.2.0, false", current, latest, hasUpdate)
	}
}

func TestExtractTarGzRejectsPathTraversal(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	content := "evil"
	if err := tw.WriteHeader(&tar.Header{
		Name: "../../etc/passwd",
		Mode: 0o644,
		Size: int64(len(content)),
	}); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatalf("write content: %v", err)
	}
	tw.Close()
	gw.Close()

	destDir := t.TempDir()
	if err := extractTarGz(&buf, destDir); err == nil {
		t.Fatal("expected error for path-traversal tar entry, got nil")
	}
}
