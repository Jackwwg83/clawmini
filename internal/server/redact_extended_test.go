package server

import (
	"reflect"
	"testing"
)

func TestRedactSensitiveArgs_DingTalkCredentials(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "dingtalk clientId",
			args: []string{"config", "set", "plugins.entries.clawdbot-dingtalk.clientId", "my-client-id"},
			want: []string{"config", "set", "plugins.entries.clawdbot-dingtalk.clientId", "******"},
		},
		{
			name: "dingtalk clientSecret",
			args: []string{"config", "set", "plugins.entries.clawdbot-dingtalk.clientSecret", "super-secret-value"},
			want: []string{"config", "set", "plugins.entries.clawdbot-dingtalk.clientSecret", "******"},
		},
		{
			name: "dingtalk aiCard enabled (not sensitive but still redacted)",
			args: []string{"config", "set", "plugins.entries.clawdbot-dingtalk.aiCard.enabled", "true"},
			want: []string{"config", "set", "plugins.entries.clawdbot-dingtalk.aiCard.enabled", "******"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactSensitiveArgs("openclaw", tc.args)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRedactSensitiveArgs_FeishuCredentials(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "feishu appId",
			args: []string{"config", "set", "plugins.entries.@anthropic-ai/feishu.appId", "my-app-id"},
			want: []string{"config", "set", "plugins.entries.@anthropic-ai/feishu.appId", "******"},
		},
		{
			name: "feishu appSecret",
			args: []string{"config", "set", "plugins.entries.@anthropic-ai/feishu.appSecret", "my-app-secret"},
			want: []string{"config", "set", "plugins.entries.@anthropic-ai/feishu.appSecret", "******"},
		},
		{
			name: "lark appId",
			args: []string{"config", "set", "plugins.entries.@anthropic-ai/lark.appId", "lark-id"},
			want: []string{"config", "set", "plugins.entries.@anthropic-ai/lark.appId", "******"},
		},
		{
			name: "lark appSecret",
			args: []string{"config", "set", "plugins.entries.@anthropic-ai/lark.appSecret", "lark-secret"},
			want: []string{"config", "set", "plugins.entries.@anthropic-ai/lark.appSecret", "******"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactSensitiveArgs("openclaw", tc.args)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRedactSensitiveArgs_NonConfigSetUnchanged(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"gateway start", []string{"gateway", "start"}},
		{"doctor json", []string{"doctor", "--json"}},
		{"channels status", []string{"channels", "status"}},
		{"plugins install", []string{"plugins", "install", "clawdbot-dingtalk"}},
		{"logs", []string{"logs"}},
		{"status json", []string{"status", "--json"}},
		{"config get", []string{"config", "get"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactSensitiveArgs("openclaw", tc.args)
			if !reflect.DeepEqual(got, tc.args) {
				t.Fatalf("non-config-set should not be modified: got %v, want %v", got, tc.args)
			}
		})
	}
}

func TestRedactSensitiveArgs_DoesNotMutateOriginal(t *testing.T) {
	original := []string{"config", "set", "key", "secret-value"}
	originalCopy := make([]string, len(original))
	copy(originalCopy, original)

	_ = RedactSensitiveArgs("openclaw", original)

	if !reflect.DeepEqual(original, originalCopy) {
		t.Fatalf("original args were mutated: %v → %v", originalCopy, original)
	}
}

func TestRedactSensitiveArgs_ShortArgs(t *testing.T) {
	// Fewer than 4 args should not panic and should not modify content
	short := [][]string{
		{"config"},
		{"config", "set"},
		{"config", "set", "key"},
	}

	for _, args := range short {
		got := RedactSensitiveArgs("openclaw", args)
		if !reflect.DeepEqual(got, args) {
			t.Fatalf("short args should be unchanged: got %v, want %v", got, args)
		}
	}

	// Empty args: returned slice may be nil, which is functionally equivalent
	got := RedactSensitiveArgs("openclaw", []string{})
	if len(got) != 0 {
		t.Fatalf("empty args should return empty: got %v", got)
	}
}

func TestRedactSensitiveArgs_NilArgs(t *testing.T) {
	got := RedactSensitiveArgs("openclaw", nil)
	if got != nil {
		t.Fatalf("expected nil for nil input, got %v", got)
	}
}
