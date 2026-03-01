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

	execUser := resolveOpenClawUser()
	execCmd, wrappedWithSudo := buildExecCommand(ctx, command, cmd.Command, cmd.Args, os.Geteuid(), execUser)
	if !wrappedWithSudo {
		// Ensure essential env vars are set (systemd services may lack them)
		execCmd.Env = ensureEnv(os.Environ())
	}
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

func buildExecCommand(ctx context.Context, resolvedCommand, originalCommand string, args []string, euid int, execUser *user.User) (*exec.Cmd, bool) {
	if shouldWrapGatewayCommandWithSudo(euid, originalCommand, args, execUser) {
		sudoArgs := make([]string, 0, 4+len(args))
		sudoArgs = append(sudoArgs, "-u", execUser.Username, "-i", resolvedCommand)
		sudoArgs = append(sudoArgs, args...)
		return exec.CommandContext(ctx, "sudo", sudoArgs...), true
	}
	return exec.CommandContext(ctx, resolvedCommand, args...), false
}

func shouldWrapGatewayCommandWithSudo(euid int, command string, args []string, execUser *user.User) bool {
	if euid != 0 {
		return false
	}
	if command != "openclaw" {
		return false
	}
	if execUser == nil || execUser.Uid == "0" || strings.TrimSpace(execUser.Username) == "" {
		return false
	}
	if len(args) < 2 || args[0] != "gateway" {
		return false
	}

	switch args[1] {
	case "start", "restart", "install", "stop", "status":
		return true
	default:
		return false
	}
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

// resolveOpenClawUser returns the actual user owning the openclaw installation.
// When running as root (systemd service), user.Current() returns root which is wrong.
// Priority: SUDO_USER env > owner of /home/*/.openclaw > user.Current()
func resolveOpenClawUser() *user.User {
	u, err := user.Current()
	if err != nil {
		return nil
	}
	if u.Uid != "0" {
		return u
	}
	// Running as root - find the real user
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		if lu, err := user.Lookup(sudoUser); err == nil {
			return lu
		}
	}
	// Check /home/*/.openclaw
	matches, _ := filepath.Glob("/home/*/.openclaw")
	for _, m := range matches {
		dir := filepath.Dir(m) // /home/username
		base := filepath.Base(dir)
		if lu, err := user.Lookup(base); err == nil {
			return lu
		}
	}
	// Check /root/.openclaw — if it exists, find the first real (non-root) user
	// with a home directory, since openclaw was likely installed via sudo
	if _, err := os.Stat("/root/.openclaw"); err == nil {
		// Find first human user (uid >= 1000) with a home in /home/
		homeDirs, _ := filepath.Glob("/home/*")
		for _, d := range homeDirs {
			base := filepath.Base(d)
			if lu, err := user.Lookup(base); err == nil && lu.Uid != "0" {
				return lu
			}
		}
	}
	// Last resort: find any user with an active systemd session (/run/user/<uid>)
	runUsers, _ := filepath.Glob("/run/user/*")
	for _, d := range runUsers {
		uid := filepath.Base(d)
		if uid == "0" {
			continue
		}
		// Lookup user by uid
		if lu, err := user.LookupId(uid); err == nil {
			return lu
		}
	}
	return u // fallback to root
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
