package main

import (
	"fmt"
	"path/filepath"
)

// swarmKeyFiles lists all swarm key files that must be copied from the repo.
var swarmKeyFiles = []string{
	"swarm.key",
	"testswarm.key",
}

// ─────────────────────────────────────────────
// copySwarmKeys copies swarm.key and testswarm.key from the cloned
// rubixgoplatform repository into the install directory.
//
// After this step the install directory contains:
//
//	rubixgoplatform[.exe]
//	ipfs[.exe]
//	swarm.key
//	testswarm.key
//
// ─────────────────────────────────────────────
func copySwarmKeys(repoPath, installPath string) error {
	for _, keyFile := range swarmKeyFiles {
		src := filepath.Join(repoPath, keyFile)
		dst := filepath.Join(installPath, keyFile)

		if !fileExists(src) {
			return fmt.Errorf("swarm key file not found in repo: %s", src)
		}

		if fileExists(dst) {
			printWarn(fmt.Sprintf("%s already exists in install dir; overwriting.", keyFile))
		}

		printProgress(fmt.Sprintf("Copying %s ...", keyFile))
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("copy %s: %w", keyFile, err)
		}
		printSuccess(fmt.Sprintf("Copied: %s → %s", src, dst))
	}
	return nil
}
