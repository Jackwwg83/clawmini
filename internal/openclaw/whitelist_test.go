package openclaw

import "testing"

func TestValidateCommand_ValidCommandsPass(t *testing.T) {
	for subCmd, allowed := range AllowedCommands {
		args := []string{subCmd}
		if !ValidateCommand("openclaw", args) {
			t.Fatalf("expected subcommand %q to be allowed", subCmd)
		}
		if len(allowed) > 0 {
			args = []string{subCmd, allowed[0]}
			if !ValidateCommand("openclaw", args) {
				t.Fatalf("expected subcommand %q with arg %q to be allowed", subCmd, allowed[0])
			}
		}
	}
}

func TestValidateCommand_InvalidCommandsRejected(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		args []string
	}{
		{name: "wrong binary", cmd: "bash", args: []string{"status"}},
		{name: "missing args", cmd: "openclaw", args: nil},
		{name: "unknown subcommand", cmd: "openclaw", args: []string{"unknown"}},
		{name: "unknown flag", cmd: "openclaw", args: []string{"status", "--evil"}},
		{name: "unvalidated positional", cmd: "openclaw", args: []string{"status", "extra"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if ValidateCommand(tc.cmd, tc.args) {
				t.Fatalf("expected validation to fail for cmd=%q args=%v", tc.cmd, tc.args)
			}
		})
	}
}

func TestValidateCommand_AllowsPositionalValuesAfterValidatedVerb(t *testing.T) {
	if !ValidateCommand("openclaw", []string{"config", "set", "gateway.mode", "prod"}) {
		t.Fatalf("expected config set with values to be allowed")
	}
}

func TestValidateCommand_IMWizardCommands(t *testing.T) {
	valid := [][]string{
		{"plugins", "install", "clawdbot-dingtalk"},
		{"plugins", "install", "@anthropic-ai/feishu"},
		{"plugins", "install", "@anthropic-ai/lark"},
		{"config", "set", "plugins.entries.clawdbot-dingtalk.clientSecret", "abc$def\\ghi"},
		{"config", "set", "plugins.entries.@anthropic-ai/feishu.appSecret", "sec$ret\\with\\slashes"},
		{"config", "set", "plugins.entries.@anthropic-ai/lark.appSecret", "sec$ret\\with\\slashes"},
		{"channels", "status"},
		{"gateway", "restart"},
	}

	for _, args := range valid {
		if !ValidateCommand("openclaw", args) {
			t.Fatalf("expected args=%v to be allowed", args)
		}
	}
}

func TestValidateCommand_RejectsExtraPositionalArgs(t *testing.T) {
	invalid := [][]string{
		{"config", "set", "plugins.entries.clawdbot-dingtalk.clientId", "abc", "extra"},
		{"plugins", "install", "clawdbot-dingtalk", "extra"},
		{"gateway", "restart", "now"},
	}

	for _, args := range invalid {
		if ValidateCommand("openclaw", args) {
			t.Fatalf("expected args=%v to be rejected", args)
		}
	}
}

func TestValidateCommand_RejectsShellInjection(t *testing.T) {
	badArgs := []string{
		"evil;rm",
		"evil|cat",
		"evil&whoami",
		"evil`id`",
	}

	for _, bad := range badArgs {
		if ValidateCommand("openclaw", []string{"status", bad}) {
			t.Fatalf("expected arg %q to be rejected", bad)
		}
	}
}
