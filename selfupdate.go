package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// selfupdate.go — `spectra --update`
//
// Fetches the latest GitHub Release for this project, downloads the matching
// platform artifact, verifies its SHA-256 against the published checksums.txt,
// then atomically replaces the running executable (rename dance so Windows
// lets us overwrite a running binary).
// ---------------------------------------------------------------------------

const (
	githubRepo    = "NamanGaonkar/SPECTRA"
	githubAPIBase = "https://api.github.com"
	applyUpdate   = true // compile-time kill switch for the updater
)

// releaseAsset is the subset of GitHub's release JSON we care about.
type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type releaseInfo struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

var errUpdateDisabled = fmt.Errorf("updater disabled in this build")

// assetNameFor returns the release artifact filename for the running platform.
func assetNameFor(tag string) string {
	name := fmt.Sprintf("spectra_%s_%s_%s", tag, runtime.GOOS, runtime.GOARCH)
	switch runtime.GOOS {
	case "windows":
		name += ".exe"
	case "darwin":
		if runtime.GOARCH == "arm64" {
			name = fmt.Sprintf("spectra_%s_macOS_apple-silicon", tag)
		} else {
			name = fmt.Sprintf("spectra_%s_macOS_intel", tag)
		}
	}
	return name
}

// githubToken returns an optional API token from the environment. With it,
// `spectra --update` also works while the repo is private (CI, dev machines);
// without it, everything is anonymous — exactly right for a public repo.
func githubToken() string {
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t
	}
	return os.Getenv("GITHUB_TOKEN")
}

// httpGet performs a GET with optional token auth and returns the body.
func httpGet(url string, timeout time.Duration) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "spectra-selfupdate")
	if t := githubToken(); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// fetchJSON performs a GET and decodes the body as JSON.
func fetchJSON(url string, out any) error {
	data, err := httpGet(url, 30*time.Second)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// fetchDownload retrieves an asset with browser_download_url.
func fetchDownload(url string) ([]byte, error) {
	return httpGet(url, 5*time.Minute)
}

// fetchChecksums downloads checksums.txt and returns name→sha256.
func fetchChecksums(tag string) (map[string]string, error) {
	raw, err := fetchDownload(fmt.Sprintf(
		"%s/%s/releases/download/%s/checksums.txt", githubAPIBase, githubRepo, tag))
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 2 {
			out[strings.TrimPrefix(filepath.Base(fields[1]), "*")] = fields[0]
		}
	}
	return out, nil
}

// latestRelease queries the GitHub API for the most recent non-draft release.
func latestRelease() (*releaseInfo, error) {
	var rel releaseInfo
	if err := fetchJSON(
		fmt.Sprintf("%s/repos/%s/releases/latest", githubAPIBase, githubRepo), &rel); err != nil {
		return nil, err
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("no published releases found")
	}
	return &rel, nil
}

// unpackRelease extracts a raw binary from a .zip or .gz artifact; plain
// binaries pass through untouched.
func unpackRelease(data []byte, name string) ([]byte, error) {
	switch {
	case strings.HasSuffix(name, ".zip"):
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if strings.EqualFold(f.Name, "spectra.exe") || strings.EqualFold(f.Name, "spectra") {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("archive %s contains no spectra binary", name)
	case strings.HasSuffix(name, ".gz"):
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		return io.ReadAll(gz)
	default:
		return data, nil
	}
}

// verifySHA256 checks data against an expected hex digest.
func verifySHA256(data []byte, want string) error {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch: got %s…, want %s…", got[:12], want[:12])
	}
	return nil
}

// runningExecutablePath resolves the real path of the current binary across
// symlinks (e.g. Homebrew shims, /usr/local/bin links).
func runningExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// replaceExecutable atomically swaps newBinary into place. On Windows the
// running image cannot be deleted, so the old file is renamed out of the way
// first; on Unix we chmod +x and rename over the original.
func replaceExecutable(path string, newBinary []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "spectra-update-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(newBinary); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		os.Remove(tmpName)
		return err
	}

	old := path + ".old"
	_ = os.Remove(old)
	if err := os.Rename(path, old); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		// roll back so we never leave the user binary-less
		_ = os.Rename(old, path)
		os.Remove(tmpName)
		return err
	}
	// best-effort cleanup of the retired image
	_ = os.Remove(old)
	return nil
}

// runSelfUpdate is the entry point for `spectra --update`.
func runSelfUpdate() error {
	if !applyUpdate {
		return errUpdateDisabled
	}

	fmt.Println("▸ checking for the latest release…")
	rel, err := latestRelease()
	if err != nil {
		return err
	}
	fmt.Printf("  latest release: %s (current: %s)\n", rel.TagName, version)

	if rel.TagName == version {
		fmt.Println("✓ already up to date.")
		return nil
	}

	want := assetNameFor(rel.TagName)
	var asset *releaseAsset
	for i := range rel.Assets {
		if rel.Assets[i].Name == want {
			asset = &rel.Assets[i]
			break
		}
	}
	if asset == nil {
		return fmt.Errorf("release %s has no artifact for %s/%s (wanted %s)",
			rel.TagName, runtime.GOOS, runtime.GOARCH, want)
	}

	fmt.Printf("▸ verifying checksums for %s…\n", asset.Name)
	sums, err := fetchChecksums(rel.TagName)
	if err != nil {
		return fmt.Errorf("could not download checksums.txt: %w", err)
	}
	expected, ok := sums[asset.Name]
	if !ok {
		return fmt.Errorf("checksums.txt has no entry for %s", asset.Name)
	}

	fmt.Printf("▸ downloading %s (%.1f MB)…\n", asset.Name, float64(asset.Size)/1e6)
	raw, err := fetchDownload(asset.URL)
	if err != nil {
		return err
	}
	if err := verifySHA256(raw, expected); err != nil {
		return err
	}
	bin, err := unpackRelease(raw, asset.Name)
	if err != nil {
		return err
	}

	fmt.Println("▸ swapping binary…")
	exePath, err := runningExecutablePath()
	if err != nil {
		return err
	}
	if err := replaceExecutable(exePath, bin); err != nil {
		return err
	}

	fmt.Printf("✓ updated to %s — restart any open spectra session.\n", rel.TagName)
	return nil
}

// checkSelfUpdate prints whether a newer release exists (used by --check).
func checkSelfUpdate() {
	rel, err := latestRelease()
	if err != nil || rel == nil {
		return // update discovery must never fail the doctor
	}
	if rel.TagName != version {
		fmt.Printf("      note: a newer release exists (%s); run `spectra --update`\n", rel.TagName)
	}
}
