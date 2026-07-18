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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
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

	assert.False(t, Installed(dataDir), "expected not installed before download")

	tag, assetURL, err := LatestRelease(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "v0.2.0", tag)

	require.NoError(t, DownloadAndInstall(t.Context(), dataDir, tag, assetURL))

	assert.True(t, Installed(dataDir), "expected installed after download")
	v, ok := InstalledVersion(dataDir)
	assert.True(t, ok)
	assert.Equal(t, "v0.2.0", v)

	_, err = os.Stat(filepath.Join(dataDir, "questions", "2018A_FE-AM.json"))
	assert.NoError(t, err, "expected extracted json file")
	_, err = os.Stat(filepath.Join(dataDir, "questions", "images", "2018A_FE-AM", "q1.png"))
	assert.NoError(t, err, "expected extracted image file")

	current, latest, hasUpdate, err := CheckUpdate(t.Context(), dataDir)
	require.NoError(t, err)
	assert.Equal(t, "v0.2.0", current)
	assert.Equal(t, "v0.2.0", latest)
	assert.False(t, hasUpdate)
}

func TestExtractTarGzRejectsPathTraversal(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	content := "evil"
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "../../etc/passwd",
		Mode: 0o644,
		Size: int64(len(content)),
	}))
	_, err := tw.Write([]byte(content))
	require.NoError(t, err)
	tw.Close()
	gw.Close()

	destDir := t.TempDir()
	assert.Error(t, extractTarGz(&buf, destDir), "expected error for path-traversal tar entry")
}
