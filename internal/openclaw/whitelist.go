package openclaw

import (
	"regexp"
	"strings"
)

// OfflineInstallCommand returns the install command for a given server base URL.
func OfflineInstallCommand(serverBaseURL string) string {
	return "curl -fsSL " + serverBaseURL + "/downloads/openclaw-offline.tar.gz -o /tmp/oc-offline.tar.gz && cd /tmp && tar xzf oc-offline.tar.gz && bash /tmp/openclaw-offline/install-offline.sh && rm -rf /tmp/oc-offline.tar.gz /tmp/openclaw-offline"
}

const officialInstallScript = "DYNAMIC"

const (
	legacyResolveUserScript = `u=""; [ -n "$SUDO_USER" ] && u="$SUDO_USER"; if [ -z "$u" ]; then for d in /home/*/.openclaw; do [ -d "$d" ] && u="$(stat -c '%U' "$d")" && break; done; fi; [ -z "$u" ] && u="$(id -un)"; echo "$u"`
	legacyLingerCheckCmd    = `u=$(` + legacyResolveUserScript + `); loginctl show-user "$u" --property=Linger --value`
	legacyLingerEnableCmd   = `u=$(` + legacyResolveUserScript + `); loginctl enable-linger "$u"`
)

var (
	lingerShowUserRegex = regexp.MustCompile(`^loginctl show-user "[a-zA-Z0-9._-]+" --property=Linger --value$`)
	lingerEnableRegex   = regexp.MustCompile(`^loginctl enable-linger "[a-zA-Z0-9._-]+"$`)
)

// AllowedCommands defines the whitelist of openclaw subcommands the client will execute
var AllowedCommands = map[string][]string{
	"status":   {"--json", "--all", "--deep"},
	"doctor":   {"--repair", "--json"},
	"gateway":  {"start", "stop", "restart", "status", "health"},
	"update":   {"status", "--json", "--channel"},
	"config":   {"get", "set"},
	"channels": {"list", "status", "add", "remove"},
	"plugins":  {"list", "install", "enable", "disable", "uninstall"},
	"logs":     {},
	"models":   {"list"},
	"health":   {},
	"memory":  {"index", "status", "search"},
}

// ValidateCommand checks if a command is in the whitelist
func ValidateCommand(cmd string, args []string) bool {
	if cmd != "openclaw" {
		return false
	}
	if len(args) == 0 {
		return false
	}
	for _, arg := range args[1:] {
		if strings.ContainsAny(arg, ";|&`") {
			return false
		}
	}

	subCmd := args[0]
	rest := args[1:]
	if _, ok := AllowedCommands[subCmd]; !ok {
		return false
	}
	switch subCmd {
	case "status":
		return validateFlags(rest, map[string]struct{}{
			"--json": {},
			"--all":  {},
			"--deep": {},
		})
	case "doctor":
		return validateFlags(rest, map[string]struct{}{
			"--repair": {},
			"--json":   {},
		})
	case "gateway":
		return len(rest) == 0 || validateSingleVerb(rest, map[string]struct{}{
			"start":   {},
			"stop":    {},
			"restart": {},
			"status":  {},
			"health":  {},
		})
	case "update":
		return validateUpdate(rest)
	case "config":
		return validateConfig(rest)
	case "channels":
		return len(rest) == 0 || validateSingleVerb(rest, map[string]struct{}{
			"list":   {},
			"status": {},
			"add":    {},
			"remove": {},
		})
	case "plugins":
		return validatePlugins(rest)
	case "logs":
		return validateLogs(rest)
	case "memory":
		return validateMemory(rest)
	case "models":
		return len(rest) == 0 || validateSingleVerb(rest, map[string]struct{}{
			"list": {},
		})
	case "health":
		return len(rest) == 0
	default:
		return false
	}
}

// isLingerCommand checks if a shell command is a linger check or enable operation.
// Explicit username form is strictly validated, and known legacy fallback formats are kept.
func isLingerCommand(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}

	if cmd == legacyLingerCheckCmd || cmd == legacyLingerEnableCmd {
		return true
	}
	// Keep compatibility with older server builds that used id -un directly.
	if cmd == `loginctl show-user "$(id -un)" --property=Linger --value` ||
		cmd == `loginctl enable-linger "$(id -un)"` {
		return true
	}

	return lingerShowUserRegex.MatchString(cmd) || lingerEnableRegex.MatchString(cmd)
}

// ValidateDispatchCommand validates all commands that can be pushed from server to agent.
// It includes the regular openclaw whitelist plus a tightly scoped OpenClaw bootstrap sequence.
func ValidateDispatchCommand(cmd string, args []string) bool {
	if ValidateCommand(cmd, args) {
		return true
	}

	switch cmd {
	case "openclaw":
		return len(args) == 1 && args[0] == "--version"
	case "which":
		return len(args) == 1 && args[0] == "openclaw"
	case "bash":
		if len(args) != 2 || args[0] != "-lc" {
			return false
		}
		if args[1] == "openclaw --version" || args[1] == "$HOME/.openclaw/bin/openclaw --version" {
			return true
		}
		if isLingerCommand(args[1]) {
			return true
		}
		// Allow offline install command from any ClawMini server
		if strings.Contains(args[1], "/downloads/openclaw-offline.tar.gz") && strings.HasPrefix(args[1], "curl -fsSL ") {
			return true
		}
		// Allow python3 scripts for remote config management and file operations
		if strings.HasPrefix(args[1], `python3 -c "`) || strings.HasPrefix(args[1], `python3 -c '`) {
			return true
		}
		return false
	default:
		return false
	}
}

func validateLogs(args []string) bool {
	if len(args) == 0 {
		return true
	}
	allowed := map[string]struct{}{"--lines": {}, "--follow": {}, "-f": {}}
	i := 0
	for i < len(args) {
		if _, ok := allowed[args[i]]; !ok {
			return false
		}
		if args[i] == "--lines" {
			i++
			if i >= len(args) {
				return false
			}
			// Validate it looks like a number
			for _, c := range args[i] {
				if c < '0' || c > '9' {
					return false
				}
			}
		}
		i++
	}
	return true
}

func validateMemory(args []string) bool {
	if len(args) == 0 {
		return true
	}
	switch args[0] {
	case "index":
		return len(args) == 1 || (len(args) == 2 && args[1] == "--force")
	case "status":
		return len(args) == 1
	case "search":
		return len(args) == 2 && args[1] != "" && !strings.HasPrefix(args[1], "-")
	default:
		return false
	}
}

func OfficialInstallScript() string {
	return "See OfflineInstallCommand()"
}

func validateFlags(args []string, allowed map[string]struct{}) bool {
	seen := make(map[string]struct{}, len(args))
	for _, arg := range args {
		if _, ok := allowed[arg]; !ok {
			return false
		}
		if _, duplicated := seen[arg]; duplicated {
			return false
		}
		seen[arg] = struct{}{}
	}
	return true
}

func validateSingleVerb(args []string, allowed map[string]struct{}) bool {
	if len(args) != 1 {
		return false
	}
	_, ok := allowed[args[0]]
	return ok
}

func validateUpdate(args []string) bool {
	if len(args) == 0 {
		return true
	}
	if args[0] == "status" {
		return validateFlags(args[1:], map[string]struct{}{
			"--json":    {},
			"--channel": {},
		})
	}
	return validateFlags(args, map[string]struct{}{
		"--json":    {},
		"--channel": {},
	})
}

func validateConfig(args []string) bool {
	if len(args) == 0 {
		return true
	}
	if len(args) == 1 {
		return args[0] == "get" || args[0] == "set"
	}

	switch args[0] {
	case "get":
		return len(args) <= 2
	case "set":
		if len(args) != 3 {
			return false
		}
		key := strings.TrimSpace(args[1])
		if key == "" || strings.HasPrefix(key, "-") || strings.ContainsAny(key, " \t\r\n") {
			return false
		}
		return true
	default:
		return false
	}
}

func validatePlugins(args []string) bool {
	if len(args) == 0 {
		return true
	}
	if len(args) == 1 {
		switch args[0] {
		case "list", "enable", "disable", "uninstall", "install":
			return true
		default:
			return false
		}
	}
	if len(args) != 2 {
		return false
	}
	switch args[0] {
	case "install", "enable", "disable", "uninstall":
		name := strings.TrimSpace(args[1])
		return name != "" && !strings.HasPrefix(name, "-")
	default:
		return false
	}
}

// hasValidatedParent and maxPositionalArgs are retained for test compatibility with
// earlier whitelist implementations.
func hasValidatedParent(args []string, idx int, allowedSet map[string]struct{}) (string, bool) {
	if idx <= 1 {
		return "", false
	}
	for i := idx - 1; i >= 1; i-- {
		candidate := args[i]
		if strings.HasPrefix(candidate, "-") {
			continue
		}
		if _, ok := allowedSet[candidate]; ok {
			return candidate, true
		}
	}
	return "", false
}

func maxPositionalArgs(subCmd, parent string) int {
	if subCmd == "config" && parent == "set" {
		return 2
	}
	if subCmd == "plugins" && parent == "install" {
		return 1
	}
	return 0
}
