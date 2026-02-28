package openclaw

import "strings"

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
}

// ValidateCommand checks if a command is in the whitelist
func ValidateCommand(cmd string, args []string) bool {
	if cmd != "openclaw" {
		return false
	}
	if len(args) == 0 {
		return false
	}
	subCmd := args[0]
	allowedArgs, ok := AllowedCommands[subCmd]
	if !ok {
		return false
	}
	allowedSet := make(map[string]struct{}, len(allowedArgs))
	for _, arg := range allowedArgs {
		allowedSet[arg] = struct{}{}
	}

	positionalCounts := make(map[string]int)

	// Reject shell injection attempts
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if strings.ContainsAny(arg, ";|&`") {
			return false
		}
		if _, ok := allowedSet[arg]; ok {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return false
		}
		// Positional values are only allowed after a validated verb.
		parent, ok := hasValidatedParent(args, i, allowedSet)
		if !ok {
			return false
		}
		positionalCounts[parent]++
		if positionalCounts[parent] > maxPositionalArgs(subCmd, parent) {
			return false
		}
	}

	return true
}

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
