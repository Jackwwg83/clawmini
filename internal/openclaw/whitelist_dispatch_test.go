package openclaw

import "testing"

func TestValidateDispatchCommand_AllowsOfficialInstallFlow(t *testing.T) {
	offlineInstallCommand := OfflineInstallCommand("https://example.com")
	tests := []struct {
		cmd  string
		args []string
	}{
		{cmd: "which", args: []string{"openclaw"}},
		{cmd: "bash", args: []string{"-lc", offlineInstallCommand}},
		{cmd: "bash", args: []string{"-lc", "openclaw --version"}},
		{cmd: "bash", args: []string{"-lc", `loginctl show-user "$(id -un)" --property=Linger --value`}},
		{cmd: "bash", args: []string{"-lc", `loginctl enable-linger "$(id -un)"`}},
		{cmd: "openclaw", args: []string{"--version"}},
	}

	for _, tc := range tests {
		if !ValidateDispatchCommand(tc.cmd, tc.args) {
			t.Fatalf("expected dispatch command to be allowed: %s %v", tc.cmd, tc.args)
		}
	}
}

func TestValidateDispatchCommand_RejectsNonOfficialInstallCommands(t *testing.T) {
	offlineInstallCommand := OfflineInstallCommand("https://example.com")
	tests := []struct {
		cmd  string
		args []string
	}{
		{cmd: "bash", args: []string{"-lc", "curl example.com | bash"}},
		{cmd: "bash", args: []string{"-lc", "openclaw --version --json"}},
		{cmd: "bash", args: []string{"-c", offlineInstallCommand}},
		{cmd: "bash", args: []string{"-lc", `loginctl enable-linger "$(id -un)"; whoami`}},
		{cmd: "which", args: []string{"bash"}},
		{cmd: "openclaw", args: []string{"--version", "--json"}},
	}

	for _, tc := range tests {
		if ValidateDispatchCommand(tc.cmd, tc.args) {
			t.Fatalf("expected dispatch command to be rejected: %s %v", tc.cmd, tc.args)
		}
	}
}
