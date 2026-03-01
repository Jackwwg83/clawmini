package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/raystone-ai/clawmini/internal/protocol"
)

type Collector struct {
	mu        sync.Mutex
	prevTotal uint64
	prevIdle  uint64
	hasCPU    bool
}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Collect(ctx context.Context, deviceID string) protocol.HeartbeatPayload {
	memTotal, memUsed := readMemInfo()
	diskTotal, diskUsed := readDisk()
	return protocol.HeartbeatPayload{
		DeviceID: deviceID,
		System: protocol.SystemInfo{
			CPUUsage:  c.cpuUsage(),
			MemTotal:  memTotal,
			MemUsed:   memUsed,
			DiskTotal: diskTotal,
			DiskUsed:  diskUsed,
			Uptime:    uptimeSeconds(),
		},
		OpenClaw: collectOpenClaw(ctx),
	}
}

func (c *Collector) cpuUsage() float64 {
	total, idle, ok := readCPUStat()
	if !ok {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.hasCPU {
		c.prevTotal = total
		c.prevIdle = idle
		c.hasCPU = true
		return 0
	}
	deltaTotal := total - c.prevTotal
	deltaIdle := idle - c.prevIdle
	c.prevTotal = total
	c.prevIdle = idle
	if deltaTotal == 0 {
		return 0
	}
	usage := 100.0 * (1.0 - float64(deltaIdle)/float64(deltaTotal))
	if usage < 0 {
		return 0
	}
	if usage > 100 {
		return 100
	}
	return usage
}

func readCPUStat() (total uint64, idle uint64, ok bool) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	if !s.Scan() {
		return 0, 0, false
	}
	fields := strings.Fields(s.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	vals := make([]uint64, 0, len(fields)-1)
	for _, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		vals = append(vals, v)
		total += v
	}
	idle = vals[3]
	if len(vals) > 4 {
		idle += vals[4]
	}
	return total, idle, true
}

func readMemInfo() (total uint64, used uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	var memTotalKB, memAvailKB uint64
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				memTotalKB, _ = strconv.ParseUint(parts[1], 10, 64)
			}
		}
		if strings.HasPrefix(line, "MemAvailable:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				memAvailKB, _ = strconv.ParseUint(parts[1], 10, 64)
			}
		}
	}
	if memTotalKB == 0 {
		return 0, 0
	}
	total = memTotalKB * 1024
	used = total - (memAvailKB * 1024)
	return total, used
}

func readDisk() (total uint64, used uint64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err != nil {
		return 0, 0
	}
	total = st.Blocks * uint64(st.Bsize)
	avail := st.Bavail * uint64(st.Bsize)
	used = total - avail
	return total, used
}

func uptimeSeconds() uint64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(b))
	if len(parts) == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(parts[0], 64)
	if err != nil || v < 0 {
		return 0
	}
	return uint64(v)
}

func collectOpenClaw(ctx context.Context) protocol.OpenClawInfo {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	installed, version := detectOpenClawInstalled(probeCtx)
	if !installed {
		return protocol.OpenClawInfo{Installed: false, GatewayStatus: "unknown"}
	}

	out, err := runOpenClawWithFallback(probeCtx, "status", "--json")
	if err != nil {
		return protocol.OpenClawInfo{Installed: true, Version: version, GatewayStatus: "unknown"}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(out, &raw); err != nil {
		return protocol.OpenClawInfo{Installed: true, Version: version, GatewayStatus: "unknown"}
	}

	info := protocol.OpenClawInfo{
		Installed:       true,
		Version:         asString(raw["version"]),
		GatewayStatus:   asString(raw["gatewayStatus"]),
		UpdateAvailable: asString(raw["updateAvailable"]),
	}
	if info.Version == "" {
		info.Version = asString(raw["openclawVersion"])
	}
	if info.Version == "" {
		info.Version = version
	}
	if info.GatewayStatus == "" {
		if g, ok := raw["gateway"].(map[string]interface{}); ok {
			info.GatewayStatus = asString(g["status"])
		}
		if info.GatewayStatus == "" {
			info.GatewayStatus = asString(raw["status"])
		}
	}
	if info.GatewayStatus == "" {
		info.GatewayStatus = "unknown"
	}

	if list, ok := raw["channels"].([]interface{}); ok {
		channels := make([]protocol.ChannelInfo, 0, len(list))
		for _, item := range list {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			ch := protocol.ChannelInfo{
				Name:   asString(m["name"]),
				Status: asString(m["status"]),
				Error:  asString(m["error"]),
			}
			if v, ok := asInt(m["messages"]); ok {
				ch.Messages = v
			}
			channels = append(channels, ch)
		}
		info.Channels = channels
	}

	return info
}

func detectOpenClawInstalled(ctx context.Context) (bool, string) {
	out, err := runOpenClawWithFallback(ctx, "--version")
	if err != nil {
		return false, ""
	}
	return true, parseOpenClawVersionText(string(out))
}

func runOpenClawWithFallback(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "openclaw", args...)
	cmd.Env = ensureEnv(os.Environ())
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}

	ocBin, pathErr := openClawBinaryPath()
	if pathErr != nil {
		return nil, err
	}
	if errors.Is(err, exec.ErrNotFound) || shouldTryDirectOpenClaw(err) {
		cmd2 := exec.CommandContext(ctx, ocBin, args...)
		cmd2.Env = ensureEnv(os.Environ())
		out2, err2 := cmd2.Output()
		if err2 == nil {
			return out2, nil
		}
		// Preserve original error for clearer behavior when both fail.
	}
	return nil, err
}

func openClawBinaryPath() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return filepath.Join(u.HomeDir, ".openclaw", "bin", "openclaw"), nil
}

func shouldTryDirectOpenClaw(err error) bool {
	var ee *exec.Error
	return errors.As(err, &ee)
}

func asString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	default:
		return ""
	}
}

func asInt(v interface{}) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int32:
		return int(x), true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case json.Number:
		n, err := x.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}
