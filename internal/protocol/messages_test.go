package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

type envelopeRaw struct {
	Type string          `json:"type"`
	ID   string          `json:"id,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

func TestEnvelopeJSONRoundTripForAllMessageTypes(t *testing.T) {
	tests := []struct {
		name     string
		msgType  string
		payload  interface{}
		newValue func() interface{}
	}{
		{
			name:    "register",
			msgType: TypeRegister,
			payload: RegisterPayload{
				DeviceID:        "dev-1",
				Hostname:        "host-a",
				Token:           "device-token",
				OS:              "linux",
				Arch:            "amd64",
				OpenClawVersion: "1.2.3",
				ClientVersion:   "0.0.1",
			},
			newValue: func() interface{} { return &RegisterPayload{} },
		},
		{
			name:    "heartbeat",
			msgType: TypeHeartbeat,
			payload: HeartbeatPayload{
				DeviceID: "dev-1",
				System: SystemInfo{
					CPUUsage:  12.5,
					MemTotal:  1024,
					MemUsed:   256,
					DiskTotal: 4096,
					DiskUsed:  2048,
					Uptime:    1234,
				},
				OpenClaw: OpenClawInfo{
					Installed:       true,
					Version:         "2.0.0",
					GatewayStatus:   "healthy",
					UpdateAvailable: "stable",
					Channels: []ChannelInfo{
						{Name: "alpha", Status: "ok", Messages: 10},
						{Name: "beta", Status: "error", Error: "timeout"},
					},
				},
			},
			newValue: func() interface{} { return &HeartbeatPayload{} },
		},
		{
			name:    "command",
			msgType: TypeCommand,
			payload: CommandPayload{
				CommandID: "cmd-1",
				Command:   "openclaw",
				Args:      []string{"status", "--json"},
				Timeout:   30,
			},
			newValue: func() interface{} { return &CommandPayload{} },
		},
		{
			name:    "result",
			msgType: TypeResult,
			payload: ResultPayload{
				CommandID:  "cmd-1",
				ExitCode:   0,
				Stdout:     "ok",
				Stderr:     "",
				DurationMs: 42,
			},
			newValue: func() interface{} { return &ResultPayload{} },
		},
		{
			name:     "ack",
			msgType:  TypeAck,
			payload:  map[string]string{"status": "registered"},
			newValue: func() interface{} { return &map[string]string{} },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := Envelope{Type: tc.msgType, ID: "msg-1", Data: tc.payload}
			rawJSON, err := json.Marshal(in)
			if err != nil {
				t.Fatalf("marshal envelope: %v", err)
			}

			var raw envelopeRaw
			if err := json.Unmarshal(rawJSON, &raw); err != nil {
				t.Fatalf("unmarshal envelope raw: %v", err)
			}
			if raw.Type != tc.msgType {
				t.Fatalf("type mismatch: got %q want %q", raw.Type, tc.msgType)
			}
			if raw.ID != "msg-1" {
				t.Fatalf("id mismatch: got %q", raw.ID)
			}

			gotPtr := tc.newValue()
			if err := json.Unmarshal(raw.Data, gotPtr); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			got := reflect.ValueOf(gotPtr).Elem().Interface()
			if !reflect.DeepEqual(got, tc.payload) {
				t.Fatalf("payload mismatch: got %#v want %#v", got, tc.payload)
			}
		})
	}
}
