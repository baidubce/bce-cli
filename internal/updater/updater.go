package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	githubAPIURL    = "https://api.github.com/repos/baidubce/bce-cli/releases/latest"
	downloadBaseURL = "https://github.com/baidubce/bce-cli/releases/download"
	apiTimeout      = 15 * time.Second
	downloadTimeout = 5 * time.Minute
)

// ReleaseInfo holds the latest release version and the download URL for the current platform.
type ReleaseInfo struct {
	Version     string // e.g. "0.2.0" (v prefix stripped)
	DownloadURL string
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// CheckLatest queries the GitHub Releases API and returns the latest release
// for the current platform.
func CheckLatest() (*ReleaseInfo, error) {
	client := &http.Client{Timeout: apiTimeout}
	req, err := http.NewRequest(http.MethodGet, githubAPIURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "bce-cli-updater")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("check for updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("parse release info: %w", err)
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("no releases found")
	}

	ver := strings.TrimPrefix(rel.TagName, "v")
	assetName := platformAssetName(ver)
	downloadURL := downloadURLFromAssets(rel.Assets, rel.TagName, assetName)

	return &ReleaseInfo{Version: ver, DownloadURL: downloadURL}, nil
}

// CompareVersions returns 1 if a > b, -1 if a < b, 0 if equal.
// Both versions may optionally start with "v".
func CompareVersions(a, b string) int {
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		var na, nb int
		if i < len(pa) {
			na, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			nb, _ = strconv.Atoi(pb[i])
		}
		if na > nb {
			return 1
		}
		if na < nb {
			return -1
		}
	}
	return 0
}

// Install downloads the archive at url, extracts the bce binary, and atomically
// replaces the currently-running executable.
func Install(url string, w io.Writer) error {
	exe, err := resolveExe()
	if err != nil {
		return err
	}

	// Download archive to a temp file.
	fmt.Fprintln(w)
	tmpArchive, err := os.CreateTemp("", "bce-upgrade-dl-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpArchive.Name())

	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Get(url)
	if err != nil {
		tmpArchive.Close()
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tmpArchive.Close()
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	if _, err := io.Copy(tmpArchive, resp.Body); err != nil {
		tmpArchive.Close()
		return fmt.Errorf("write download: %w", err)
	}
	tmpArchive.Close()

	// Extract the bce binary from the archive.
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	binData, err := extractBinary(tmpArchive.Name(), ext)
	if err != nil {
		return fmt.Errorf("extract binary: %w", err)
	}

	// Replace the current executable.
	if err := atomicReplace(exe, binData); err != nil {
		return err
	}
	return nil
}

// DownloadURLForVersion returns the download URL for a specific version.
// ver may optionally include the "v" prefix (e.g. "0.3.0" or "v0.3.0").
func DownloadURLForVersion(ver string) string {
	tag := ver
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	return fmt.Sprintf("%s/%s/%s", downloadBaseURL, tag, platformAssetName(strings.TrimPrefix(tag, "v")))
}

// platformAssetName returns the archive filename for the current OS/arch.
func platformAssetName(ver string) string {
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macosx"
	}
	return fmt.Sprintf("bce-%s-%s-%s.%s", osName, ver, runtime.GOARCH, ext)
}

// downloadURLFromAssets returns the browser_download_url for assetName from the
// assets list, or falls back to constructing the URL from the release tag.
func downloadURLFromAssets(assets []githubAsset, tag, assetName string) string {
	for _, a := range assets {
		if a.Name == assetName {
			return a.BrowserDownloadURL
		}
	}
	return fmt.Sprintf("%s/%s/%s", downloadBaseURL, tag, assetName)
}

// resolveExe returns the real path of the currently-running executable,
// following symlinks.
func resolveExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate current binary: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks: %w", err)
	}
	return exe, nil
}

// extractBinary opens the archive at path and returns the raw bytes of the
// bce (or bce.exe) binary found inside.
func extractBinary(path, ext string) ([]byte, error) {
	if ext == ".zip" {
		return extractFromZip(path)
	}
	return extractFromTarGz(path)
}

func extractFromTarGz(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		base := filepath.Base(hdr.Name)
		if base == "bce" || base == "bce.exe" {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("bce binary not found in archive")
}

func extractFromZip(path string) ([]byte, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		base := filepath.Base(f.Name)
		if base == "bce" || base == "bce.exe" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("bce binary not found in archive")
}

// atomicReplace writes data to the exe path, using a platform-appropriate strategy.
func atomicReplace(exe string, data []byte) error {
	if runtime.GOOS == "windows" {
		return atomicReplaceWindows(exe, data)
	}
	return atomicReplaceUnix(exe, data)
}

func atomicReplaceUnix(exe string, data []byte) error {
	tmp := exe + ".upgrade.tmp"
	if err := os.WriteFile(tmp, data, 0755); err != nil {
		os.Remove(tmp)
		if os.IsPermission(err) {
			return fmt.Errorf("permission denied: try running with sudo")
		}
		return fmt.Errorf("write new binary: %w", err)
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		if os.IsPermission(err) {
			return fmt.Errorf("permission denied: try running with sudo")
		}
		return fmt.Errorf("replace binary: %w", err)
	}
	return nil
}

func atomicReplaceWindows(exe string, data []byte) error {
	old := exe + ".old"
	// Move the running binary aside (Windows allows renaming a running exe).
	if err := os.Rename(exe, old); err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("permission denied: run as administrator")
		}
		return fmt.Errorf("rename current binary: %w", err)
	}
	if err := os.WriteFile(exe, data, 0755); err != nil {
		// Try to restore the original.
		os.Rename(old, exe)
		return fmt.Errorf("write new binary: %w", err)
	}
	// Best-effort cleanup; the file may still be in use.
	os.Remove(old)
	return nil
}
