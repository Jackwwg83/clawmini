package client

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectOpenClawBinary_NotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	installed, version := DetectOpenClawBinary(context.Background())
	if installed {
		t.Fatalf("expected installed=false when binary is missing")
	}
	if version != "" {
		t.Fatalf("expected empty version when binary is missing, got %q", version)
	}
}

func TestDetectOpenClawBinary_WithVersion(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "openclaw")
	if err := os.WriteFile(bin, []byte("#!/usr/bin/env bash\necho 'openclaw v3.2.1'\n"), 0o755); err != nil {
		t.Fatalf("write mock openclaw: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	installed, version := DetectOpenClawBinary(context.Background())
	if !installed {
		t.Fatalf("expected installed=true")
	}
	if version != "openclaw v3.2.1" {
		t.Fatalf("unexpected version: %q", version)
	}
}

func TestParseOpenClawVersionText(t *testing.T) {
	got := parseOpenClawVersionText("\n  \nopenclaw 4.0.0\n")
	if got != "openclaw 4.0.0" {
		t.Fatalf("unexpected parsed version: %q", got)
	}
}
