package server

import (
	"reflect"
	"testing"
)

func TestRedactSensitiveArgs(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    []string
		want    []string
	}{
		{
			name:    "config set redacts value",
			command: "openclaw",
			args:    []string{"config", "set", "plugins.entries.foo.secret", "my-secret"},
			want:    []string{"config", "set", "plugins.entries.foo.secret", "******"},
		},
		{
			name:    "other openclaw command unchanged",
			command: "openclaw",
			args:    []string{"plugins", "install", "@openclaw/feishu"},
			want:    []string{"plugins", "install", "@openclaw/feishu"},
		},
		{
			name:    "other binaries unchanged",
			command: "bash",
			args:    []string{"-c", "echo test"},
			want:    []string{"-c", "echo test"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactSensitiveArgs(tc.command, tc.args)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("RedactSensitiveArgs() = %v, want %v", got, tc.want)
			}
			if &got[0] == &tc.args[0] {
				t.Fatalf("expected returned args to be copied")
			}
		})
	}
}
