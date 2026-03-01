package main

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/raystone-ai/clawmini/internal/server"
)

// resolveUserScript finds the actual user who owns the openclaw installation.
// When the client runs as root (systemd service), id -un returns "root" which is wrong.
// Priority: SUDO_USER > owner of /home/*/.openclaw > fallback id -un
const (
	resolveUserScript = `u=""; [ -n "$SUDO_USER" ] && u="$SUDO_USER"; if [ -z "$u" ]; then for d in /home/*/.openclaw; do [ -d "$d" ] && u="$(stat -c '%U' "$d")" && break; done; fi; [ -z "$u" ] && u="$(id -un)"; echo "$u"`

	lingerCheckCommand  = `u=$(` + resolveUserScript + `); loginctl show-user "$u" --property=Linger --value`
	lingerEnableCommand = `u=$(` + resolveUserScript + `); loginctl enable-linger "$u"`
)

var lingerUsernamePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

func formatLingerCheckCommand(username string) string {
	return fmt.Sprintf(`loginctl show-user "%s" --property=Linger --value`, username)
}

func formatLingerEnableCommand(username string) string {
	return fmt.Sprintf(`loginctl enable-linger "%s"`, username)
}

func resolveLingerCommands(deviceID, username string) (string, string) {
	username = strings.TrimSpace(username)
	if username != "" && lingerUsernamePattern.MatchString(username) {
		return formatLingerCheckCommand(username), formatLingerEnableCommand(username)
	}

	if username == "" {
		log.Printf("WARNING: device %s heartbeat username is empty, falling back to runtime user detection for linger commands", strings.TrimSpace(deviceID))
	} else {
		log.Printf("WARNING: device %s heartbeat username %q is invalid, falling back to runtime user detection for linger commands", strings.TrimSpace(deviceID), username)
	}
	return lingerCheckCommand, lingerEnableCommand
}

// ensureLingerEnabled verifies user linger state and enables it when required.
// jobID/stepKey/adminIP are accepted for call-site consistency across flows.
func (a *serverApp) ensureLingerEnabled(jobID, deviceID, stepKey, adminIP, username string) (server.CommandRecord, error) {
	lingerCheck, lingerEnable := resolveLingerCommands(deviceID, username)

	lingerCheckRec, lingerCheckErr := a.dispatchAndWaitCommand(deviceID, "bash", []string{"-lc", lingerCheck}, 20)
	if lingerCheckErr != nil {
		return lingerCheckRec, fmt.Errorf("%s: %w", lingerErrorPrefix(jobID, stepKey, adminIP, "检查 linger 状态失败"), lingerCheckErr)
	}
	if isCommandFailed(lingerCheckRec) {
		errText := commandErrorText(lingerCheckRec, "检查 linger 状态失败")
		return lingerCheckRec, errors.New(lingerErrorPrefix(jobID, stepKey, adminIP, errText))
	}

	lingerState := strings.ToLower(strings.TrimSpace(lingerCheckRec.Stdout))
	if lingerState == "" {
		lingerState = strings.ToLower(strings.TrimSpace(lingerCheckRec.Stderr))
	}
	if lingerState == "yes" {
		return lingerCheckRec, nil
	}

	lingerEnableRec, lingerEnableErr := a.dispatchAndWaitCommand(deviceID, "bash", []string{"-lc", lingerEnable}, 20)
	if lingerEnableErr != nil {
		return lingerEnableRec, fmt.Errorf("%s: %w", lingerErrorPrefix(jobID, stepKey, adminIP, "启用 linger 失败"), lingerEnableErr)
	}
	if isCommandFailed(lingerEnableRec) {
		errText := commandErrorText(lingerEnableRec, "启用 linger 失败")
		return lingerEnableRec, errors.New(lingerErrorPrefix(jobID, stepKey, adminIP, errText))
	}

	return lingerEnableRec, nil
}

func (a *serverApp) lookupDeviceUsername(deviceID string) string {
	return a.devices.GetDeviceUsername(deviceID)
}

func requiresGatewayLinger(command string, args []string) bool {
	if strings.ToLower(strings.TrimSpace(command)) != "openclaw" {
		return false
	}
	if len(args) < 2 || strings.ToLower(strings.TrimSpace(args[0])) != "gateway" {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(args[1])) {
	case "start", "restart", "install":
		return true
	default:
		return false
	}
}

func lingerErrorPrefix(jobID, stepKey, adminIP, fallback string) string {
	message := strings.TrimSpace(fallback)
	if jobID == "" && stepKey == "" && adminIP == "" {
		return message
	}

	parts := make([]string, 0, 2)
	if jobID != "" {
		parts = append(parts, "job="+jobID)
	}
	if stepKey != "" {
		parts = append(parts, "step="+stepKey)
	}
	if adminIP != "" {
		parts = append(parts, "adminIP="+adminIP)
	}
	return strings.TrimSpace(message + " (" + strings.Join(parts, ", ") + ")")
}

func isLikelyDeviceOfflineError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, "offline") || strings.Contains(text, "not connected")
}
