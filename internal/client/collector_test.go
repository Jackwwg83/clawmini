package client

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"
)

func writeMockOpenClaw(t *testing.T, script string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "openclaw")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write mock openclaw: %v", err)
	}
	return dir
}

func TestCollectorCollect_SystemInfo(t *testing.T) {
	c := NewCollector()

	hb1 := c.Collect(context.Background(), "dev-1")
	if hb1.DeviceID != "dev-1" {
		t.Fatalf("device id mismatch: got %q", hb1.DeviceID)
	}
	if hb1.System.Username == "" {
		t.Fatalf("username should be populated in heartbeat system info")
	}
	if hb1.System.CPUUsage < 0 || hb1.System.CPUUsage > 100 {
		t.Fatalf("cpu usage out of range: %v", hb1.System.CPUUsage)
	}
	if hb1.System.MemTotal < hb1.System.MemUsed {
		t.Fatalf("mem invariant violated: total=%d used=%d", hb1.System.MemTotal, hb1.System.MemUsed)
	}
	if hb1.System.DiskTotal < hb1.System.DiskUsed {
		t.Fatalf("disk invariant violated: total=%d used=%d", hb1.System.DiskTotal, hb1.System.DiskUsed)
	}
	if hb1.OpenClaw.GatewayStatus == "" {
		t.Fatalf("gateway status should always be set")
	}

	time.Sleep(20 * time.Millisecond)
	hb2 := c.Collect(context.Background(), "dev-1")
	if hb2.System.CPUUsage < 0 || hb2.System.CPUUsage > 100 {
		t.Fatalf("second cpu usage out of range: %v", hb2.System.CPUUsage)
	}
}

func TestCollectorCollect_UsesResolvedOpenClawUser(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Fatalf("current user: %v", err)
	}

	t.Setenv("SUDO_USER", current.Username)

	hb := NewCollector().Collect(context.Background(), "dev-user")
	if hb.System.Username != current.Username {
		t.Fatalf("expected username %q, got %q", current.Username, hb.System.Username)
	}
}

func TestCollectOpenClaw_ParsesJSONOutput(t *testing.T) {
	mockDir := writeMockOpenClaw(t, `#!/bin/sh
cat <<'EOF'
{"openclawVersion":"2.3.4","gateway":{"status":"healthy"},"updateAvailable":"stable","channels":[{"name":"alpha","status":"ok","messages":7}]}
EOF
`)
	t.Setenv("PATH", mockDir+":"+os.Getenv("PATH"))

	info := collectOpenClaw(context.Background())
	if !info.Installed {
		t.Fatalf("expected openclaw to be detected as installed")
	}
	if info.Version != "2.3.4" {
		t.Fatalf("version parse mismatch: got %q", info.Version)
	}
	if info.GatewayStatus != "healthy" {
		t.Fatalf("gateway status parse mismatch: got %q", info.GatewayStatus)
	}
	if info.UpdateAvailable != "stable" {
		t.Fatalf("update channel mismatch: got %q", info.UpdateAvailable)
	}
	if len(info.Channels) != 1 {
		t.Fatalf("expected one channel, got %d", len(info.Channels))
	}
	if info.Channels[0].Name != "alpha" || info.Channels[0].Messages != 7 {
		t.Fatalf("channel parse mismatch: %+v", info.Channels[0])
	}
}

func TestCollectOpenClaw_NotInstalledFallback(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	info := collectOpenClaw(context.Background())
	if info.Installed {
		t.Fatalf("expected installed=false when command missing")
	}
	if info.GatewayStatus != "unknown" {
		t.Fatalf("expected unknown gateway fallback, got %q", info.GatewayStatus)
	}
}

func TestCollectOpenClaw_StatusFailureStillInstalled(t *testing.T) {
	mockDir := writeMockOpenClaw(t, `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "openclaw v9.9.9"
  exit 0
fi
if [ "$1" = "status" ] && [ "$2" = "--json" ]; then
  echo "gateway not configured" >&2
  exit 1
fi
exit 1
`)
	t.Setenv("PATH", mockDir+":"+os.Getenv("PATH"))

	info := collectOpenClaw(context.Background())
	if !info.Installed {
		t.Fatalf("expected installed=true when --version succeeds")
	}
	if info.Version != "openclaw v9.9.9" {
		t.Fatalf("expected version from --version probe, got %q", info.Version)
	}
	if info.GatewayStatus != "unknown" {
		t.Fatalf("expected unknown gateway status when status fails, got %q", info.GatewayStatus)
	}
}
