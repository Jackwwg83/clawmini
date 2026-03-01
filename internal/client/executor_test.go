package client

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/raystone-ai/clawmini/internal/protocol"
)

func TestExecutorRejectsNonWhitelistedCommand(t *testing.T) {
	exec := NewExecutor()
	result := exec.Execute(context.Background(), protocol.CommandPayload{
		CommandID: "cmd-reject",
		Command:   "bash",
		Args:      []string{"-c", "echo hacked"},
		Timeout:   5,
	})

	if result.ExitCode != 126 {
		t.Fatalf("expected exit code 126 for rejected command, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "whitelist") {
		t.Fatalf("expected whitelist rejection error, got %q", result.Stderr)
	}
}

func TestExecutorExecutesAllowedCommand(t *testing.T) {
	mockDir := writeMockOpenClaw(t, `#!/bin/sh
echo "ARGS:$@"
`)
	t.Setenv("PATH", mockDir+":"+os.Getenv("PATH"))

	exec := NewExecutor()
	result := exec.Execute(context.Background(), protocol.CommandPayload{
		CommandID: "cmd-ok",
		Command:   "openclaw",
		Args:      []string{"status", "--json"},
		Timeout:   5,
	})

	if result.ExitCode != 0 {
		t.Fatalf("expected success exit code, got %d stderr=%q", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "ARGS:status --json") {
		t.Fatalf("unexpected stdout: %q", result.Stdout)
	}
}

func TestExecutorTimeout(t *testing.T) {
	mockDir := writeMockOpenClaw(t, `#!/bin/sh
sleep 2
`)
	t.Setenv("PATH", mockDir+":"+os.Getenv("PATH"))

	exec := NewExecutor()
	result := exec.Execute(context.Background(), protocol.CommandPayload{
		CommandID: "cmd-timeout",
		Command:   "openclaw",
		Args:      []string{"logs"},
		Timeout:   1,
	})

	if result.ExitCode != 124 {
		t.Fatalf("expected timeout exit code 124, got %d stderr=%q", result.ExitCode, result.Stderr)
	}
	if result.Stderr == "" {
		t.Fatalf("expected timeout stderr message")
	}
}

func TestExecutorCapsStdoutWithTruncationMarker(t *testing.T) {
	mockDir := writeMockOpenClaw(t, `#!/bin/sh
dd if=/dev/zero bs=2048 count=1024 2>/dev/null | tr '\0' 'a'
`)
	t.Setenv("PATH", mockDir+":"+os.Getenv("PATH"))

	exec := NewExecutor()
	result := exec.Execute(context.Background(), protocol.CommandPayload{
		CommandID: "cmd-cap-stdout",
		Command:   "openclaw",
		Args:      []string{"logs"},
		Timeout:   5,
	})

	if result.ExitCode != 0 {
		t.Fatalf("expected success exit code, got %d stderr=%q", result.ExitCode, result.Stderr)
	}
	if len(result.Stdout) > int(maxCapturedOutputBytes) {
		t.Fatalf("stdout len=%d exceeds cap=%d", len(result.Stdout), maxCapturedOutputBytes)
	}
	if !strings.HasSuffix(result.Stdout, truncatedOutputMarker) {
		t.Fatalf("expected truncation marker suffix, got tail=%q", result.Stdout[max(0, len(result.Stdout)-32):])
	}
}

func TestExecutorCapsStderrWithTruncationMarker(t *testing.T) {
	mockDir := writeMockOpenClaw(t, `#!/bin/sh
dd if=/dev/zero bs=2048 count=1024 2>/dev/null | tr '\0' 'b' 1>&2
`)
	t.Setenv("PATH", mockDir+":"+os.Getenv("PATH"))

	exec := NewExecutor()
	result := exec.Execute(context.Background(), protocol.CommandPayload{
		CommandID: "cmd-cap-stderr",
		Command:   "openclaw",
		Args:      []string{"logs"},
		Timeout:   5,
	})

	if result.ExitCode != 0 {
		t.Fatalf("expected success exit code, got %d stderr=%q", result.ExitCode, result.Stderr)
	}
	if len(result.Stderr) > int(maxCapturedOutputBytes) {
		t.Fatalf("stderr len=%d exceeds cap=%d", len(result.Stderr), maxCapturedOutputBytes)
	}
	if !strings.HasSuffix(result.Stderr, truncatedOutputMarker) {
		t.Fatalf("expected truncation marker suffix")
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
