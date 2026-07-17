package core

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// dataReleaseAPI is a var (not const) so tests can point it at a local
// httptest.Server instead of the real GitHub API.
var dataReleaseAPI = "https://api.github.com/repos/jim-ww/itpec-sensei/releases/latest"

const (
	dataAssetName   = "data.tar.gz"
	dataVersionFile = "version.txt"
)

// Installed reports whether question data has already been downloaded into dataDir.
func Installed(dataDir string) bool {
	_, ok := InstalledVersion(dataDir)
	return ok
}

// InstalledVersion returns the release tag of the currently-installed data, if any.
func InstalledVersion(dataDir string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(dataDir, dataVersionFile))
	if err != nil {
		return "", false
	}
	v := strings.TrimSpace(string(raw))
	if v == "" {
		return "", false
	}
	return v, true
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// LatestRelease queries GitHub for the latest release and the download URL of
// its data archive asset.
func LatestRelease(ctx context.Context) (tag, assetURL string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dataReleaseAPI, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("fetch latest release: unexpected status %s", resp.Status)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", fmt.Errorf("decode latest release: %w", err)
	}
	for _, a := range rel.Assets {
		if a.Name == dataAssetName {
			return rel.TagName, a.BrowserDownloadURL, nil
		}
	}
	return "", "", fmt.Errorf("latest release %s has no %s asset", rel.TagName, dataAssetName)
}

// CheckUpdate compares the installed data version against the latest release.
func CheckUpdate(ctx context.Context, dataDir string) (current, latest string, hasUpdate bool, err error) {
	current, _ = InstalledVersion(dataDir)
	latest, _, err = LatestRelease(ctx)
	if err != nil {
		return current, "", false, err
	}
	return current, latest, current != latest, nil
}

// DownloadAndInstall downloads the tar.gz asset at assetURL and extracts it into
// dataDir/questions, then records tag as the installed version. Extraction lands
// in a temp directory first and is only swapped into place once it fully
// succeeds, so a failed/interrupted download never leaves a half-installed bank.
func DownloadAndInstall(ctx context.Context, dataDir, tag, assetURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", assetURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: unexpected status %s", assetURL, resp.Status)
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("mkdir data dir: %w", err)
	}
	tmpDir, err := os.MkdirTemp(dataDir, ".download-*")
	if err != nil {
		return fmt.Errorf("create temp extract dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := extractTarGz(resp.Body, tmpDir); err != nil {
		return fmt.Errorf("extract %s: %w", dataAssetName, err)
	}

	questionsDir := filepath.Join(dataDir, "questions")
	if err := os.RemoveAll(questionsDir); err != nil {
		return fmt.Errorf("remove old questions dir: %w", err)
	}
	if err := os.Rename(tmpDir, questionsDir); err != nil {
		return fmt.Errorf("install questions dir: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dataDir, dataVersionFile), []byte(tag+"\n"), 0o644); err != nil {
		return fmt.Errorf("write version file: %w", err)
	}
	return nil
}

// extractTarGz extracts a gzip-compressed tar stream into destDir, guarding
// against path traversal ("zip-slip") from malformed or malicious archives.
func extractTarGz(r io.Reader, destDir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}

		target := filepath.Join(destDir, filepath.Clean(hdr.Name))
		if !strings.HasPrefix(target, destDir+string(filepath.Separator)) && target != destDir {
			return fmt.Errorf("tar entry escapes destination: %s", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
	}
}
