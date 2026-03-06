package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ─────────────────────────────────────────────
// Banner & step printing
// ─────────────────────────────────────────────

// printBanner prints the ASCII welcome banner.
func printBanner() {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║              RUBIX SETUP — Automated Node Installer       ║")
	fmt.Println("║         rubixgoplatform + IPFS Kubo v0.19.1               ║")
	fmt.Println("╚══════════════════════════════════════════════════════════╝")
	fmt.Println()
}

// printStep prints a numbered step header.
func printStep(n int, title string) {
	fmt.Printf("\n═══════════════════════════════════════\n")
	fmt.Printf("  Step %d: %s\n", n, title)
	fmt.Printf("═══════════════════════════════════════\n")
}

// printProgress prints a progress line prefixed with ">>>".
func printProgress(msg string) {
	fmt.Printf(">>> %s\n", msg)
}

// printSuccess prints a success line prefixed with "[OK]".
func printSuccess(msg string) {
	fmt.Printf("[OK] %s\n", msg)
}

// printWarn prints a warning line.
func printWarn(msg string) {
	fmt.Printf("[WARN] %s\n", msg)
}

// ─────────────────────────────────────────────
// Interactive input helpers
// ─────────────────────────────────────────────

var stdinReader = bufio.NewReader(os.Stdin)

// readLine prints a prompt and reads a single line from stdin.
// Leading/trailing whitespace is trimmed.
func readLine(prompt string) string {
	fmt.Print(prompt)
	line, _ := stdinReader.ReadString('\n')
	return strings.TrimSpace(line)
}

// readLineDefault prints a prompt and returns defaultVal if the user enters nothing.
func readLineDefault(prompt, defaultVal string) string {
	val := readLine(prompt)
	if val == "" {
		return defaultVal
	}
	return val
}

// readInt reads an integer from stdin that falls within [min, max].
// It loops until valid input is received.
func readInt(prompt string, min, max int) int {
	for {
		raw := readLine(prompt)
		n, err := strconv.Atoi(raw)
		if err != nil || n < min || n > max {
			fmt.Printf("    Please enter a number between %d and %d.\n", min, max)
			continue
		}
		return n
	}
}

// readIntDefault is like readInt but returns defaultVal on empty input.
func readIntDefault(prompt string, min, max, defaultVal int) int {
	for {
		raw := readLine(prompt)
		if raw == "" {
			return defaultVal
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < min || n > max {
			fmt.Printf("    Please enter a number between %d and %d (or press Enter for %d).\n", min, max, defaultVal)
			continue
		}
		return n
	}
}

// readYesNo reads a Y/n response. Returns true for yes (default).
func readYesNo(prompt string) bool {
	raw := strings.ToLower(readLine(prompt))
	return raw == "" || raw == "y" || raw == "yes"
}

// ─────────────────────────────────────────────
// Port availability
// ─────────────────────────────────────────────

// isPortAvailable returns true if the given TCP port is not in use on localhost.
func isPortAvailable(port int) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// ─────────────────────────────────────────────
// File system helpers
// ─────────────────────────────────────────────

// ensureDir creates all directories in path if they do not already exist.
func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

// copyFile copies the file at src to dst, creating dst if necessary.
// Existing dst is overwritten.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source %q: %w", src, err)
	}
	defer in.Close()

	// Preserve executable bit from source
	srcInfo, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat source %q: %w", src, err)
	}

	if err := ensureDir(filepath.Dir(dst)); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("create dest %q: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %q → %q: %w", src, dst, err)
	}
	return nil
}

// fileExists returns true if the given path exists (file or directory).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// getExecutableDir returns the directory where the current binary lives.
// Falls back to the current working directory on error.
func getExecutableDir() string {
	exe, err := os.Executable()
	if err != nil {
		cwd, _ := os.Getwd()
		return cwd
	}
	return filepath.Dir(exe)
}

// getCurrentDir returns the current working directory.
func getCurrentDir() string {
	cwd, _ := os.Getwd()
	return cwd
}

// absPath converts any path (relative or absolute) to an absolute path
// anchored at the current working directory.
// Examples:
//
//	"test"        → "/Users/gokul/.../RubixNodeGenerator/test"
//	"../rubix"    → "/Users/gokul/.../rubix"
//	"/tmp/rubix"  → "/tmp/rubix"  (unchanged, already absolute)
func absPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p // return as-is if resolution fails
	}
	return abs
}

// ─────────────────────────────────────────────
// Download progress writer
// ─────────────────────────────────────────────

// progressWriter wraps an io.Writer and prints download progress to stdout.
type progressWriter struct {
	total     int64
	written   int64
	lastPrint time.Time
	dst       io.Writer
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.dst.Write(p)
	pw.written += int64(n)
	now := time.Now()
	if now.Sub(pw.lastPrint) > 500*time.Millisecond {
		if pw.total > 0 {
			pct := float64(pw.written) / float64(pw.total) * 100
			fmt.Printf("\r    Downloading... %.1f MB / %.1f MB (%.0f%%)   ",
				float64(pw.written)/1e6, float64(pw.total)/1e6, pct)
		} else {
			fmt.Printf("\r    Downloading... %.1f MB   ", float64(pw.written)/1e6)
		}
		pw.lastPrint = now
	}
	return n, err
}

// ─────────────────────────────────────────────
// OS / arch helpers
// ─────────────────────────────────────────────

// executableName returns the platform-correct binary name.
// On Windows it appends ".exe".
func executableName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}
