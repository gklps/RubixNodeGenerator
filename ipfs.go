package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	ipfsVersion = "v0.19.1"
	ipfsBaseURL = "https://dist.ipfs.tech/kubo/" + ipfsVersion + "/"
)

// ─────────────────────────────────────────────
// downloadIPFS downloads IPFS Kubo, extracts the binary,
// and places it in installPath.
//
// Cache behaviour:
//  1. If ipfs binary already exists in installPath → skip entirely.
//  2. If a cached binary exists in ~/.rubix-setup/cache/… → copy from cache (no download).
//  3. Otherwise → download, extract, copy to installPath, then save to cache.
//
// ─────────────────────────────────────────────
func downloadIPFS(installPath string) error {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	printProgress(fmt.Sprintf("IPFS Kubo %s for %s/%s", ipfsVersion, goos, goarch))

	binaryName := executableName("ipfs")
	dstBinary := filepath.Join(installPath, binaryName)

	// ── 1. Already installed in this installPath → nothing to do ─────────────
	if fileExists(dstBinary) {
		printWarn(fmt.Sprintf("IPFS binary already exists at %s; skipping.", dstBinary))
		return nil
	}

	// ── 2. Check local cache ──────────────────────────────────────────────────
	cachePath := ipfsCachePath(goos, goarch)
	printProgress(fmt.Sprintf("Checking local cache: %s", cachePath))

	if fileExists(cachePath) {
		printSuccess("Cache hit — copying IPFS binary from local cache (no download needed).")
		if err := copyFile(cachePath, dstBinary); err != nil {
			return fmt.Errorf("copy from cache: %w", err)
		}
		printSuccess(fmt.Sprintf("IPFS installed: %s", dstBinary))
		return nil
	}

	// ── 3. Download, extract, install, then cache ─────────────────────────────
	url, archiveName, isZip := ipfsDownloadURL(goos, goarch)
	printProgress(fmt.Sprintf("No cache found. Downloading: %s", url))

	tmpDir, err := os.MkdirTemp("", "rubix-ipfs-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, archiveName)
	if err := downloadFile(url, archivePath); err != nil {
		return fmt.Errorf("download IPFS: %w", err)
	}
	fmt.Println() // newline after progress bar

	extractDir := filepath.Join(tmpDir, "extracted")
	if err := ensureDir(extractDir); err != nil {
		return err
	}

	if isZip {
		if err := extractZip(archivePath, extractDir); err != nil {
			return fmt.Errorf("extract zip: %w", err)
		}
	} else {
		if err := extractTarGz(archivePath, extractDir); err != nil {
			return fmt.Errorf("extract tar.gz: %w", err)
		}
	}

	// Kubo extracts to: <extractDir>/kubo/ipfs[.exe]
	srcBinary := filepath.Join(extractDir, "kubo", binaryName)
	if !fileExists(srcBinary) {
		srcBinary, err = findBinaryInDir(extractDir, binaryName)
		if err != nil {
			return fmt.Errorf("ipfs binary not found after extraction: %w", err)
		}
	}

	// Copy to install path
	printProgress(fmt.Sprintf("Installing IPFS binary to %s ...", dstBinary))
	if err := copyFile(srcBinary, dstBinary); err != nil {
		return fmt.Errorf("copy ipfs binary: %w", err)
	}
	printSuccess(fmt.Sprintf("IPFS installed: %s", dstBinary))

	// Save to cache for future runs (non-fatal if it fails)
	if err := saveToCache(srcBinary, cachePath); err != nil {
		printWarn(fmt.Sprintf("Could not save to cache (%v); future runs will re-download.", err))
	} else {
		printSuccess(fmt.Sprintf("IPFS cached at: %s", cachePath))
	}

	return nil
}

// ─────────────────────────────────────────────
// ipfsCachePath returns the path where the IPFS binary is cached locally.
//
// Structure:
//
//	~/.rubix-setup/cache/ipfs/<version>/<os>-<arch>/ipfs[.exe]
//
// Using the version + platform in the path means:
//   - Different versions coexist without conflict.
//   - Different OS/arch builds on a shared home dir (NFS etc.) stay separate.
//
// ─────────────────────────────────────────────
func ipfsCachePath(goos, goarch string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback: use temp dir (cache won't persist across reboots, but won't panic)
		home = os.TempDir()
	}
	return filepath.Join(
		home,
		".rubix-setup", "cache", "ipfs",
		ipfsVersion,
		goos+"-"+goarch,
		executableName("ipfs"),
	)
}

// saveToCache copies the extracted binary into the cache directory.
// The cache directory is created if it does not exist.
func saveToCache(srcBinary, cachePath string) error {
	if err := ensureDir(filepath.Dir(cachePath)); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	if err := copyFile(srcBinary, cachePath); err != nil {
		return fmt.Errorf("write cache: %w", err)
	}
	return nil
}

// ─────────────────────────────────────────────
// ipfsDownloadURL returns the download URL, archive filename, and whether
// the archive is a zip (true) or tar.gz (false).
// ─────────────────────────────────────────────
func ipfsDownloadURL(goos, goarch string) (url, filename string, isZip bool) {
	// Map Go OS names to IPFS dist names
	osName := goos // linux, darwin, windows — match directly

	// Map Go arch names to IPFS dist arch names
	archName := goarch // amd64, arm64 — match directly

	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
		isZip = true
	}

	// Example: kubo_v0.19.1_linux-amd64.tar.gz
	filename = fmt.Sprintf("kubo_%s_%s-%s.%s", ipfsVersion, osName, archName, ext)
	url = ipfsBaseURL + filename
	return url, filename, isZip
}

// ─────────────────────────────────────────────
// downloadFile downloads src URL to dst file path, with a progress indicator.
// ─────────────────────────────────────────────
func downloadFile(url, dst string) error {
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return fmt.Errorf("HTTP GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer out.Close()

	pw := &progressWriter{
		total: resp.ContentLength,
		dst:   out,
	}

	if _, err := io.Copy(pw, resp.Body); err != nil {
		return fmt.Errorf("write download: %w", err)
	}
	return nil
}

// ─────────────────────────────────────────────
// extractTarGz extracts a .tar.gz archive into dst directory.
// Protects against path traversal (e.g. ../../evil).
// ─────────────────────────────────────────────
func extractTarGz(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}

		// Sanitise path
		target, err := safePath(dst, header.Name)
		if err != nil {
			continue // skip suspicious entries
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := ensureDir(target); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := ensureDir(filepath.Dir(target)); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
				os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
	return nil
}

// ─────────────────────────────────────────────
// extractZip extracts a .zip archive into dst directory.
// Protects against path traversal.
// ─────────────────────────────────────────────
func extractZip(src, dst string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		target, err := safePath(dst, f.Name)
		if err != nil {
			continue
		}

		if f.FileInfo().IsDir() {
			ensureDir(target) //nolint:errcheck
			continue
		}

		if err := ensureDir(filepath.Dir(target)); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// safePath joins dst and name and verifies the result is within dst.
// Returns an error if the combined path would escape dst (path traversal).
func safePath(dst, name string) (string, error) {
	target := filepath.Join(dst, filepath.FromSlash(name))
	// Ensure the target is inside dst
	if !strings.HasPrefix(target, filepath.Clean(dst)+string(os.PathSeparator)) {
		// Allow exact match (e.g. dst itself)
		if target != filepath.Clean(dst) {
			return "", fmt.Errorf("path traversal detected: %s", name)
		}
	}
	return target, nil
}

// findBinaryInDir searches one level deep in dir for a file named name.
func findBinaryInDir(dir, name string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			candidate := filepath.Join(dir, e.Name(), name)
			if fileExists(candidate) {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("binary %q not found under %s", name, dir)
}
