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
	_, ok := AllowedCommands[subCmd]
	if !ok {
		return false
	}
	// Reject shell injection attempts
	for _, arg := range args {
		if strings.ContainsAny(arg, ";|&$`\\") {
			return false
		}
	}
	return true
}
