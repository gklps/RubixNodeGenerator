package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ─────────────────────────────────────────────
// buildProject compiles rubixgoplatform for the current OS/arch
// and copies the resulting binary into installPath.
//
// Strategy:
//  1. Try `make compile-{os}` (uses the repo Makefile).
//  2. Fall back to direct `go build` with env-var overrides
//     (avoids the global `go env -w` side-effect in the Makefile).
//
// ─────────────────────────────────────────────
func buildProject(repoPath, installPath string, cfg *Config) error {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	printProgress(fmt.Sprintf("Detected platform: %s/%s", goos, goarch))

	// Determine the Makefile target and expected output path
	makeTarget, relBinPath, finalBinName := buildTargets(goos, goarch)

	printProgress(fmt.Sprintf("Building rubixgoplatform (%s) ...", makeTarget))

	// ── Attempt 1: make ──────────────────────────────────────────────────────
	if makePath, err := exec.LookPath("make"); err == nil {
		err := runMake(makePath, makeTarget, repoPath, goos, goarch)
		if err == nil {
			return copyBuiltBinary(repoPath, relBinPath, installPath, finalBinName)
		}
		printWarn(fmt.Sprintf("make failed (%v); falling back to direct go build.", err))
	} else {
		printWarn("make not found; using direct go build.")
	}

	// ── Attempt 2: direct go build ───────────────────────────────────────────
	if err := runGoBuild(repoPath, relBinPath, goos, goarch); err != nil {
		return fmt.Errorf("go build failed: %w", err)
	}

	return copyBuiltBinary(repoPath, relBinPath, installPath, finalBinName)
}

// buildTargets returns the Makefile target name, the relative output path within
// the repo, and the final binary name to use in the install directory.
func buildTargets(goos, goarch string) (makeTarget, relBinPath, finalBinName string) {
	switch goos {
	case "linux":
		return "compile-linux",
			filepath.Join("linux", "rubixgoplatform"),
			"rubixgoplatform"

	case "windows":
		return "compile-windows",
			filepath.Join("windows", "rubixgoplatform.exe"),
			"rubixgoplatform.exe"

	case "darwin":
		// The Makefile compile-mac hardcodes arm64; for amd64 we always use
		// direct go build (even if make succeeds it would produce the wrong arch).
		if goarch == "arm64" {
			return "compile-mac",
				filepath.Join("mac", "rubixgoplatform"),
				"rubixgoplatform"
		}
		// amd64 Mac — use direct build, not the Makefile target
		return "compile-mac-amd64",
			filepath.Join("mac", "rubixgoplatform"),
			"rubixgoplatform"

	default:
		return "compile-linux",
			filepath.Join("linux", "rubixgoplatform"),
			"rubixgoplatform"
	}
}

// runMake runs `make <target>` inside repoPath.
// For macOS amd64 the compile-mac-amd64 target does not exist in the Makefile,
// so we return an error immediately to trigger the fallback.
func runMake(makePath, target, repoPath, goos, goarch string) error {
	if goos == "darwin" && goarch == "amd64" {
		return fmt.Errorf("no amd64 mac target in Makefile; use direct build")
	}

	cmd := exec.Command(makePath, target)
	cmd.Dir = repoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runGoBuild compiles the project directly using `go build` with the correct
// environment variables set per-process (no global go env mutation).
func runGoBuild(repoPath, relOutputPath, goos, goarch string) error {
	outputPath := filepath.Join(repoPath, relOutputPath)

	// Ensure the output subdirectory exists (linux/, windows/, mac/)
	if err := ensureDir(filepath.Dir(outputPath)); err != nil {
		return err
	}

	args := []string{"build", "-o", outputPath, "."}
	cmd := exec.Command("go", args...)
	cmd.Dir = repoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Build the environment: inherit current env, then override GOOS/GOARCH/CGO.
	cmd.Env = buildEnv(goos, goarch)

	printProgress(fmt.Sprintf("Running: go build -o %s (GOOS=%s GOARCH=%s)", relOutputPath, goos, goarch))
	return cmd.Run()
}

// buildEnv returns an os.Environ() slice with GOOS, GOARCH, and CGO_ENABLED set.
// We strip any existing values for those keys from the current environment so
// that our explicit settings always win.
func buildEnv(goos, goarch string) []string {
	base := os.Environ()

	skip := map[string]bool{
		"GOOS":        true,
		"GOARCH":      true,
		"CGO_ENABLED": true,
	}

	clean := make([]string, 0, len(base)+3)
	for _, e := range base {
		// e has the form KEY=value
		idx := strings.IndexByte(e, '=')
		if idx > 0 && skip[e[:idx]] {
			continue
		}
		clean = append(clean, e)
	}

	clean = append(clean,
		"GOOS="+goos,
		"GOARCH="+goarch,
		"CGO_ENABLED=1",
	)
	return clean
}

// copyBuiltBinary copies the compiled binary from the repo into installPath,
// then ad-hoc codesigns it on macOS (required by macOS 15 dyld).
func copyBuiltBinary(repoPath, relBinPath, installPath, finalBinName string) error {
	src := filepath.Join(repoPath, relBinPath)
	dst := filepath.Join(installPath, finalBinName)

	if !fileExists(src) {
		return fmt.Errorf("compiled binary not found at %s", src)
	}

	printProgress(fmt.Sprintf("Copying binary to %s ...", dst))
	if err := copyFile(src, dst); err != nil {
		return fmt.Errorf("copy binary: %w", err)
	}

	// macOS 15 (Sequoia) dyld requires LC_UUID which CGO-built binaries lack.
	// Ad-hoc codesigning injects the required load command automatically.
	if runtime.GOOS == "darwin" {
		if err := codesignBinary(dst); err != nil {
			// Non-fatal: warn and continue; user may codesign manually.
			printWarn(fmt.Sprintf("codesign failed (%v); nodes may not start on macOS 15.", err))
		}
	}

	printSuccess(fmt.Sprintf("Binary installed: %s", dst))
	return nil
}

// codesignBinary performs an ad-hoc code signature on the given binary.
// This is required on macOS 15+ for CGO-built executables.
func codesignBinary(path string) error {
	printProgress("Applying ad-hoc codesign (required on macOS 15+)...")

	// Clear quarantine attribute first (in case binary came from downloads)
	xattr := exec.Command("xattr", "-cr", path)
	_ = xattr.Run() // best-effort

	// Ad-hoc sign with "-" identity (no Developer ID required)
	sign := exec.Command("codesign", "--sign", "-", "--force", path)
	if out, err := sign.CombinedOutput(); err != nil {
		return fmt.Errorf("codesign: %s: %w", strings.TrimSpace(string(out)), err)
	}
	printSuccess("Binary codesigned successfully.")
	return nil
}
