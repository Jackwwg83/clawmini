package openclaw

import "testing"

func TestValidateDispatchCommand_AllowsOfficialInstallFlow(t *testing.T) {
	tests := []struct {
		cmd  string
		args []string
	}{
		{cmd: "which", args: []string{"openclaw"}},
		{cmd: "bash", args: []string{"-lc", OfficialInstallScript()}},
		{cmd: "bash", args: []string{"-lc", "openclaw --version"}},
		{cmd: "openclaw", args: []string{"--version"}},
	}

	for _, tc := range tests {
		if !ValidateDispatchCommand(tc.cmd, tc.args) {
			t.Fatalf("expected dispatch command to be allowed: %s %v", tc.cmd, tc.args)
		}
	}
}

func TestValidateDispatchCommand_RejectsNonOfficialInstallCommands(t *testing.T) {
	tests := []struct {
		cmd  string
		args []string
	}{
		{cmd: "bash", args: []string{"-lc", "curl example.com | bash"}},
		{cmd: "bash", args: []string{"-lc", "openclaw --version --json"}},
		{cmd: "bash", args: []string{"-c", OfficialInstallScript()}},
		{cmd: "which", args: []string{"bash"}},
		{cmd: "openclaw", args: []string{"--version", "--json"}},
	}

	for _, tc := range tests {
		if ValidateDispatchCommand(tc.cmd, tc.args) {
			t.Fatalf("expected dispatch command to be rejected: %s %v", tc.cmd, tc.args)
		}
	}
}
