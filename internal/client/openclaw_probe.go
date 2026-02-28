package client

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

const openClawVersionProbeTimeout = 5 * time.Second

// DetectOpenClawBinary checks whether openclaw exists and returns its version text when available.
func DetectOpenClawBinary(parent context.Context) (bool, string) {
	if _, err := exec.LookPath("openclaw"); err != nil {
		return false, ""
	}

	ctx, cancel := context.WithTimeout(parent, openClawVersionProbeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "openclaw", "--version").CombinedOutput()
	if err != nil {
		return true, ""
	}
	return true, parseOpenClawVersionText(string(out))
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
