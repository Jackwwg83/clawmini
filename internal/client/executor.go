package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/raystone-ai/clawmini/internal/openclaw"
	"github.com/raystone-ai/clawmini/internal/protocol"
)

type Executor struct{}

const maxCapturedOutputBytes int64 = 1 << 20

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

	var buf bytes.Buffer
	limited := &io.LimitedReader{R: r, N: maxBytes}
	if _, err := io.Copy(&buf, limited); err != nil {
		return "", err
	}
	if limited.N == 0 {
		if _, err := io.Copy(io.Discard, r); err != nil {
			return "", err
		}
	}
	return buf.String(), nil
}
