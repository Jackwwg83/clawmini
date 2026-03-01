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

	execCmd := exec.CommandContext(ctx, cmd.Command, cmd.Args...)
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

// ensureEnv makes sure HOME, USER, and PATH are set in the environment.
// systemd services often run with minimal env, causing scripts to fail.
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

	if !has["HOME"] {
		if u, err := user.Current(); err == nil {
			env = append(env, "HOME="+u.HomeDir)
		}
	}
	if !has["USER"] {
		if u, err := user.Current(); err == nil {
			env = append(env, "USER="+u.Username)
		}
	}
	if !has["PATH"] {
		env = append(env, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	}
	return env
}
