package client

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

const openClawVersionProbeTimeout = 5 * time.Second

// DetectOpenClawBinary checks whether openclaw exists and returns its version text when available.
func DetectOpenClawBinary(parent context.Context) (bool, string) {
	ctx, cancel := context.WithTimeout(parent, openClawVersionProbeTimeout)
	defer cancel()

	// Try with enhanced PATH first
	cmd := exec.CommandContext(ctx, "openclaw", "--version")
	cmd.Env = ensureEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, parseOpenClawVersionText(string(out))
	}

	// Fallback: try ~/.openclaw/bin/openclaw directly
	if u, uerr := user.Current(); uerr == nil {
		ocBin := filepath.Join(u.HomeDir, ".openclaw", "bin", "openclaw")
		cmd2 := exec.CommandContext(ctx, ocBin, "--version")
		cmd2.Env = ensureEnv(os.Environ())
		out2, err2 := cmd2.CombinedOutput()
		if err2 == nil {
			return true, parseOpenClawVersionText(string(out2))
		}
	}

	return false, ""
}

func parseOpenClawVersionText(raw string) string {
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
