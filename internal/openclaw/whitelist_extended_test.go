package openclaw

import "testing"

// ============================================================
// Sprint 2A: Extended whitelist tests for all device detail features
// ============================================================

func TestValidateCommand_AllGatewayOperations(t *testing.T) {
	operations := []string{"start", "stop", "restart", "status", "health"}
	for _, op := range operations {
		if !ValidateCommand("openclaw", []string{"gateway", op}) {
			t.Errorf("expected gateway %s to be allowed", op)
		}
	}
}

func TestValidateCommand_GatewayRejectsPositionalArgs(t *testing.T) {
	invalid := [][]string{
		{"gateway", "start", "extra"},
		{"gateway", "stop", "now"},
		{"gateway", "restart", "--force"},
		{"gateway", "status", "--verbose"},
	}
	for _, args := range invalid {
		if ValidateCommand("openclaw", args) {
			t.Errorf("expected args=%v to be rejected", args)
		}
	}
}

func TestValidateCommand_DoctorCombinedFlags(t *testing.T) {
	valid := [][]string{
		{"doctor"},
		{"doctor", "--json"},
		{"doctor", "--repair"},
		{"doctor", "--repair", "--json"},
		{"doctor", "--json", "--repair"},
	}
	for _, args := range valid {
		if !ValidateCommand("openclaw", args) {
			t.Errorf("expected args=%v to be allowed", args)
		}
	}

	invalid := [][]string{
		{"doctor", "--verbose"},
		{"doctor", "--fix"},
		{"doctor", "--json", "extra"},
	}
	for _, args := range invalid {
		if ValidateCommand("openclaw", args) {
			t.Errorf("expected args=%v to be rejected", args)
		}
	}
}

func TestValidateCommand_UpdateCombinedFlags(t *testing.T) {
	valid := [][]string{
		{"update", "status"},
		{"update", "--json"},
		{"update", "status", "--json"},
		{"update", "--channel"},
		{"update", "status", "--json", "--channel"},
	}
	for _, args := range valid {
		if !ValidateCommand("openclaw", args) {
			t.Errorf("expected args=%v to be allowed", args)
		}
	}

	invalid := [][]string{
		{"update", "--force"},
		{"update", "install"},
	}
	for _, args := range invalid {
		if ValidateCommand("openclaw", args) {
			t.Errorf("expected args=%v to be rejected", args)
		}
	}
}

func TestValidateCommand_ChannelsAllOperations(t *testing.T) {
	operations := []string{"list", "status", "add", "remove"}
	for _, op := range operations {
		if !ValidateCommand("openclaw", []string{"channels", op}) {
			t.Errorf("expected channels %s to be allowed", op)
		}
	}

	invalid := [][]string{
		{"channels", "delete"},
		{"channels", "create"},
		{"channels", "status", "--verbose"},
	}
	for _, args := range invalid {
		if ValidateCommand("openclaw", args) {
			t.Errorf("expected args=%v to be rejected", args)
		}
	}
}

func TestValidateCommand_PluginsAllOperations(t *testing.T) {
	// Without positional arg
	for _, op := range []string{"list", "enable", "disable", "uninstall"} {
		if !ValidateCommand("openclaw", []string{"plugins", op}) {
			t.Errorf("expected plugins %s to be allowed", op)
		}
	}

	// Install with plugin name (1 positional)
	validInstalls := [][]string{
		{"plugins", "install", "clawdbot-dingtalk"},
		{"plugins", "install", "@anthropic-ai/feishu"},
		{"plugins", "install", "@anthropic-ai/lark"},
		{"plugins", "install", "some-plugin-name"},
	}
	for _, args := range validInstalls {
		if !ValidateCommand("openclaw", args) {
			t.Errorf("expected args=%v to be allowed", args)
		}
	}
}

func TestValidateCommand_PluginsInstallRejectsMultipleNames(t *testing.T) {
	if ValidateCommand("openclaw", []string{"plugins", "install", "plugin1", "plugin2"}) {
		t.Fatalf("expected multiple plugin names to be rejected")
	}
}

func TestValidateCommand_ConfigGetNoPositionalArgs(t *testing.T) {
	if !ValidateCommand("openclaw", []string{"config", "get"}) {
		t.Fatalf("expected 'config get' to be allowed")
	}
	// Config get should not allow positional args (maxPositionalArgs returns 0 for get)
	// Actually let's check: hasValidatedParent finds "get" as parent, maxPositionalArgs("config","get")=0
	// So config get key should be rejected
	if ValidateCommand("openclaw", []string{"config", "get", "some.key"}) {
		t.Fatalf("expected 'config get some.key' to be rejected")
	}
}

func TestValidateCommand_ConfigSetMaxTwoPositional(t *testing.T) {
	// config set KEY VALUE — allowed (2 positional)
	if !ValidateCommand("openclaw", []string{"config", "set", "key", "value"}) {
		t.Fatalf("expected config set with key+value to be allowed")
	}

	// config set KEY VALUE EXTRA — rejected (3 positional)
	if ValidateCommand("openclaw", []string{"config", "set", "key", "value", "extra"}) {
		t.Fatalf("expected config set with 3 positional args to be rejected")
	}
}

func TestValidateCommand_LogsNoArgs(t *testing.T) {
	if !ValidateCommand("openclaw", []string{"logs"}) {
		t.Fatalf("expected 'logs' to be allowed")
	}
	// Any args should be rejected
	if ValidateCommand("openclaw", []string{"logs", "--tail"}) {
		t.Fatalf("expected 'logs --tail' to be rejected")
	}
	if ValidateCommand("openclaw", []string{"logs", "100"}) {
		t.Fatalf("expected 'logs 100' to be rejected")
	}
}

func TestValidateCommand_HealthNoArgs(t *testing.T) {
	if !ValidateCommand("openclaw", []string{"health"}) {
		t.Fatalf("expected 'health' to be allowed")
	}
	if ValidateCommand("openclaw", []string{"health", "--verbose"}) {
		t.Fatalf("expected 'health --verbose' to be rejected")
	}
}

func TestValidateCommand_ModelsListOnly(t *testing.T) {
	if !ValidateCommand("openclaw", []string{"models", "list"}) {
		t.Fatalf("expected 'models list' to be allowed")
	}
	if ValidateCommand("openclaw", []string{"models", "delete"}) {
		t.Fatalf("expected 'models delete' to be rejected")
	}
	// "models" alone is valid since it's a whitelisted subcommand
	// (same as "logs", "health", etc.)
	if !ValidateCommand("openclaw", []string{"models"}) {
		t.Fatalf("expected 'models' alone to be valid")
	}
}

func TestValidateCommand_StatusWithFlags(t *testing.T) {
	valid := [][]string{
		{"status"},
		{"status", "--json"},
		{"status", "--all"},
		{"status", "--deep"},
		{"status", "--json", "--all"},
		{"status", "--json", "--all", "--deep"},
	}
	for _, args := range valid {
		if !ValidateCommand("openclaw", args) {
			t.Errorf("expected args=%v to be allowed", args)
		}
	}

	invalid := [][]string{
		{"status", "--verbose"},
		{"status", "--format"},
	}
	for _, args := range invalid {
		if ValidateCommand("openclaw", args) {
			t.Errorf("expected args=%v to be rejected", args)
		}
	}
}

func TestValidateCommand_ShellInjectionVariations(t *testing.T) {
	injections := []string{
		"$(whoami)",      // Not caught (no ;|&` chars), but doesn't need to be — these are args, not shell
		"foo;bar",        // Semicolon
		"foo|bar",        // Pipe
		"foo&bar",        // Ampersand
		"foo`bar`",       // Backtick
		";",              // Just semicolon
		"|",              // Just pipe
		"&",              // Just ampersand
		"`",              // Just backtick
		"a;b;c",          // Multiple semicolons
		"test|grep pass", // Common injection
	}
	for _, injection := range injections {
		if ValidateCommand("openclaw", []string{"status", injection}) {
			t.Errorf("expected injection %q to be rejected", injection)
		}
	}
}

func TestValidateCommand_EmptyArgsRejected(t *testing.T) {
	if ValidateCommand("openclaw", []string{}) {
		t.Fatalf("expected empty args to be rejected")
	}
	if ValidateCommand("openclaw", nil) {
		t.Fatalf("expected nil args to be rejected")
	}
}

func TestValidateCommand_NonOpenclawBinaries(t *testing.T) {
	binaries := []string{"bash", "sh", "python", "curl", "rm", "sudo", "chmod", ""}
	for _, bin := range binaries {
		if ValidateCommand(bin, []string{"status"}) {
			t.Errorf("expected binary %q to be rejected", bin)
		}
	}
}

func TestValidateCommand_SpecialCharactersInConfigValues(t *testing.T) {
	// Config set values can contain special characters (except shell injection chars)
	valid := [][]string{
		{"config", "set", "key", "value-with-dashes"},
		{"config", "set", "key", "value_with_underscores"},
		{"config", "set", "key", "value.with.dots"},
		{"config", "set", "key", "value/with/slashes"},
		{"config", "set", "key", "value@with@at"},
		{"config", "set", "key", "value$with$dollar"},
		{"config", "set", "key", "value\\with\\backslash"},
		{"config", "set", "key", "value with spaces"}, // Note: space is NOT a shell injection char
	}
	for _, args := range valid {
		if !ValidateCommand("openclaw", args) {
			t.Errorf("expected args=%v to be allowed", args)
		}
	}
}

func TestValidateCommand_IMConfigPluginNames(t *testing.T) {
	// All plugin names used in IM configuration should pass
	pluginNames := []string{
		"clawdbot-dingtalk",
		"@anthropic-ai/feishu",
		"@anthropic-ai/lark",
	}
	for _, name := range pluginNames {
		if !ValidateCommand("openclaw", []string{"plugins", "install", name}) {
			t.Errorf("expected plugin install %q to be allowed", name)
		}
	}

	// Plugin names that contain shell injection chars should be rejected
	badNames := []string{
		"plugin;evil",
		"plugin|evil",
		"plugin&evil",
		"plugin`evil`",
	}
	for _, name := range badNames {
		if ValidateCommand("openclaw", []string{"plugins", "install", name}) {
			t.Errorf("expected malicious plugin name %q to be rejected", name)
		}
	}
}

func TestValidateCommand_IMConfigSetCommands(t *testing.T) {
	// Full set of config set commands used in IM wizard
	valid := [][]string{
		// DingTalk
		{"config", "set", "plugins.entries.clawdbot-dingtalk.clientId", "some-client-id"},
		{"config", "set", "plugins.entries.clawdbot-dingtalk.clientSecret", "some-secret"},
		{"config", "set", "plugins.entries.clawdbot-dingtalk.aiCard.enabled", "true"},
		// Feishu
		{"config", "set", "plugins.entries.@anthropic-ai/feishu.appId", "app-id"},
		{"config", "set", "plugins.entries.@anthropic-ai/feishu.appSecret", "app-secret"},
		// Lark
		{"config", "set", "plugins.entries.@anthropic-ai/lark.appId", "app-id"},
		{"config", "set", "plugins.entries.@anthropic-ai/lark.appSecret", "app-secret"},
	}
	for _, args := range valid {
		if !ValidateCommand("openclaw", args) {
			t.Errorf("expected IM config set args=%v to be allowed", args)
		}
	}
}

func TestHasValidatedParent_EdgeCases(t *testing.T) {
	allowedSet := map[string]struct{}{
		"set":     {},
		"install": {},
		"--json":  {},
	}

	// idx=0 should return false (no parent possible)
	_, ok := hasValidatedParent([]string{"set"}, 0, allowedSet)
	if ok {
		t.Fatalf("expected no parent for idx=0")
	}

	// idx=1 should return false (idx-1=0 is not in scope for subcommand args)
	_, ok = hasValidatedParent([]string{"config", "value"}, 1, allowedSet)
	if ok {
		t.Fatalf("expected no parent for idx=1")
	}

	// idx=2 with validated parent at idx=1
	parent, ok := hasValidatedParent([]string{"config", "set", "key"}, 2, allowedSet)
	if !ok || parent != "set" {
		t.Fatalf("expected parent 'set', got %q ok=%v", parent, ok)
	}

	// Skip flags to find parent
	parent, ok = hasValidatedParent([]string{"config", "install", "--json", "plugin"}, 3, allowedSet)
	if !ok || parent != "install" {
		t.Fatalf("expected parent 'install', got %q ok=%v", parent, ok)
	}
}

func TestMaxPositionalArgs(t *testing.T) {
	tests := []struct {
		subCmd, parent string
		want           int
	}{
		{"config", "set", 2},
		{"config", "get", 0},
		{"plugins", "install", 1},
		{"plugins", "list", 0},
		{"plugins", "enable", 0},
		{"gateway", "start", 0},
		{"channels", "status", 0},
		{"logs", "", 0},
	}
	for _, tc := range tests {
		got := maxPositionalArgs(tc.subCmd, tc.parent)
		if got != tc.want {
			t.Errorf("maxPositionalArgs(%q, %q) = %d, want %d", tc.subCmd, tc.parent, got, tc.want)
		}
	}
}
