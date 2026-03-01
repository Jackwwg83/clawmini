package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/raystone-ai/clawmini/internal/openclaw"
	"github.com/raystone-ai/clawmini/internal/protocol"
)

type Executor struct{}

const maxCapturedOutputBytes int64 = 1 << 20
const truncatedOutputMarker = "\n[output truncated]\n"

func NewExecutor() *Executor {
	return &Executor{}
}

func (e *Executor) Execute(parent context.Context, cmd protocol.CommandPayload) protocol.ResultPayload {
	start := time.Now()
	result := protocol.ResultPayload{CommandID: cmd.CommandID}

	if !openclaw.ValidateDispatchCommand(cmd.Command, cmd.Args) {
		result.ExitCode = 126
		result.Stderr = "command rejected by whitelist"
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	timeout := time.Duration(cmd.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	command := cmd.Command
	if resolved, err := resolveCommand(command); err == nil {
		command = resolved
	}

	execCmd := exec.CommandContext(ctx, command, cmd.Args...)
	// Ensure essential env vars are set (systemd services may lack them)
	execCmd.Env = ensureEnv(os.Environ())
	stdoutPipe, err := execCmd.StdoutPipe()
	if err != nil {
		result.ExitCode = 1
		result.Stderr = err.Error()
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}
	stderrPipe, err := execCmd.StderrPipe()
	if err != nil {
		result.ExitCode = 1
		result.Stderr = err.Error()
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	if err := execCmd.Start(); err != nil {
		result.ExitCode = 1
		result.Stderr = err.Error()
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	type streamResult struct {
		text string
		err  error
	}
	stdoutCh := make(chan streamResult, 1)
	stderrCh := make(chan streamResult, 1)
	go func() {
		text, readErr := readCappedOutput(stdoutPipe, maxCapturedOutputBytes)
		stdoutCh <- streamResult{text: text, err: readErr}
	}()
	go func() {
		text, readErr := readCappedOutput(stderrPipe, maxCapturedOutputBytes)
		stderrCh <- streamResult{text: text, err: readErr}
	}()

	err = execCmd.Wait()
	stdout := <-stdoutCh
	stderr := <-stderrCh

	result.Stdout = stdout.text
	result.Stderr = stderr.text
	result.ExitCode = 0
	if err != nil {
		result.ExitCode = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.ExitCode = 124
			if result.Stderr == "" {
				result.Stderr = fmt.Sprintf("command timed out after %s", timeout)
			}
		} else if result.Stderr == "" {
			result.Stderr = err.Error()
		}
	}
	if stderr.err != nil && result.Stderr == "" {
		result.Stderr = fmt.Sprintf("stderr read failed: %v", stderr.err)
	}
	if stdout.err != nil && result.Stderr == "" {
		result.Stderr = fmt.Sprintf("stdout read failed: %v", stdout.err)
	}

	result.DurationMs = time.Since(start).Milliseconds()
	return result
}

func readCappedOutput(r io.ReadCloser, maxBytes int64) (string, error) {
	defer r.Close()

	if maxBytes <= 0 {
		maxBytes = 1
	}

	markerBytes := []byte(truncatedOutputMarker)
	captureLimit := maxBytes - int64(len(markerBytes))
	if captureLimit < 0 {
		captureLimit = 0
	}

	var buf bytes.Buffer
	limited := &io.LimitedReader{R: r, N: captureLimit + 1}
	if _, err := io.Copy(&buf, limited); err != nil {
		return "", err
	}
	if int64(buf.Len()) > captureLimit {
		capped := buf.Bytes()
		if captureLimit > 0 {
			capped = capped[:captureLimit]
		} else {
			capped = capped[:0]
		}
		if _, err := io.Copy(io.Discard, r); err != nil {
			return "", err
		}
		return string(capped) + truncatedOutputMarker, nil
	}
	return buf.String(), nil
}

// ensureEnv makes sure HOME, USER, PATH, and user-systemd env vars are set.
// systemd services often run with minimal env, causing scripts to fail.

// resolveOpenClawUser returns the runtime user.
// For root services this is intentionally root.
func resolveOpenClawUser() *user.User {
	u, err := user.Current()
	if err != nil {
		return nil
	}
	return u
}

func ensureEnv(env []string) []string {
	has := make(map[string]bool)
	for _, e := range env {
		for i := 0; i < len(e); i++ {
			if e[i] == '=' {
				has[e[:i]] = true
				break
			}
		}
	}

	u := resolveOpenClawUser()
	if !has["HOME"] && u != nil {
		env = append(env, "HOME="+u.HomeDir)
	}
	if !has["USER"] && u != nil {
		env = append(env, "USER="+u.Username)
	}
	if !has["XDG_RUNTIME_DIR"] && u != nil {
		env = append(env, fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%s", u.Uid))
	}
	if !has["DBUS_SESSION_BUS_ADDRESS"] && u != nil {
		env = append(env, fmt.Sprintf("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%s/bus", u.Uid))
	}
	// Ensure PATH includes common install locations
	homeDir := ""
	if u != nil {
		homeDir = u.HomeDir
	}
	if !has["PATH"] {
		p := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
		if homeDir != "" {
			p = homeDir + "/.openclaw/bin:" + homeDir + "/.npm-global/bin:" + homeDir + "/.local/bin:" + p
		}
		env = append(env, "PATH="+p)
	} else if homeDir != "" {
		// PATH exists but may not include openclaw install dirs - prepend them
		for i, e := range env {
			if len(e) > 5 && e[:5] == "PATH=" {
				env[i] = "PATH=" + homeDir + "/.openclaw/bin:" + homeDir + "/.npm-global/bin:" + homeDir + "/.local/bin:" + e[5:]
				break
			}
		}
	}
	return env
}

func resolveCommand(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("command name is empty")
	}

	if strings.ContainsRune(name, os.PathSeparator) {
		if isExecutableFile(name) {
			return name, nil
		}
		return "", fmt.Errorf("command not found: %s", name)
	}

	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}

	env := ensureEnv(os.Environ())
	if resolved, err := lookPathInPath(name, envValue(env, "PATH")); err == nil {
		return resolved, nil
	}

	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("command not found: %s", name)
	}
	candidates := []string{
		filepath.Join(u.HomeDir, ".openclaw", "bin", name),
		filepath.Join(u.HomeDir, ".npm-global", "bin", name),
		filepath.Join(u.HomeDir, ".local", "bin", name),
	}
	for _, candidate := range candidates {
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("command not found: %s", name)
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return entry[len(prefix):]
		}
	}
	return ""
}

func lookPathInPath(name, pathValue string) (string, error) {
	if pathValue == "" {
		return "", exec.ErrNotFound
	}
	for _, dir := range filepath.SplitList(pathValue) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, name)
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}
