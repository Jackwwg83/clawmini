package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/raystone-ai/clawmini/internal/protocol"
)

// testHubServerWithJoinTokens sets up a Hub with JoinTokenStore enabled.
func testHubServerWithJoinTokens(t *testing.T) (*Hub, *JoinTokenStore, *DeviceStore, *CommandStore, string) {
	t.Helper()

	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema: %v", err)
	}
	if _, err := users.EnsureDefaultAdmin(); err != nil {
		t.Fatalf("ensure default admin: %v", err)
	}
	commands := NewCommandStore(db)
	if err := commands.EnsureSchema(); err != nil {
		t.Fatalf("ensure command schema: %v", err)
	}
	joinTokens := NewJoinTokenStore(db)
	if err := joinTokens.EnsureSchema(); err != nil {
		t.Fatalf("ensure join token schema: %v", err)
	}

	auth := NewTokenAuth("device-token", []byte("hub-test-secret"), users)
	hub := NewHub(devices, commands, joinTokens, users, auth)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.HandleDeviceWS)
	mux.HandleFunc("/api/ws", hub.HandleBrowserWS)
	baseHTTPURL := startTCP4HTTPServer(t, mux)

	base := "ws" + strings.TrimPrefix(baseHTTPURL, "http")
	return hub, joinTokens, devices, commands, base + "/ws"
}

func TestHubRejectsExpiredJoinToken(t *testing.T) {
	hub, joinTokens, _, _, deviceWSURL := testHubServerWithJoinTokens(t)

	tok, err := joinTokens.CreateToken("expire-test", time.Hour, "")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	// Force the token to be expired
	db := joinTokens.db
	if _, err := db.Exec(`UPDATE join_tokens SET expires_at = ? WHERE id = ?;`, nowUnix()-10, tok.ID); err != nil {
		t.Fatalf("force expire: %v", err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(deviceWSURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(protocol.Envelope{
		Type: protocol.TypeRegister,
		Data: protocol.RegisterPayload{
			DeviceID:      "dev-expired",
			Hostname:      "host-expired",
			Token:         tok.ID,
			OS:            "linux",
			Arch:          "amd64",
			ClientVersion: "0.1.0",
		},
	}); err != nil {
		t.Fatalf("send register: %v", err)
	}

	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatalf("expected connection close for expired join token")
	}

	if hub.IsDeviceOnline("dev-expired") {
		t.Fatalf("device with expired join token should not be online")
	}
}

func TestHubRejectsNonExistentJoinToken(t *testing.T) {
	hub, _, _, _, deviceWSURL := testHubServerWithJoinTokens(t)

	conn, _, err := websocket.DefaultDialer.Dial(deviceWSURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(protocol.Envelope{
		Type: protocol.TypeRegister,
		Data: protocol.RegisterPayload{
			DeviceID:      "dev-fake",
			Hostname:      "host-fake",
			Token:         "totally-fake-token-that-does-not-exist",
			OS:            "linux",
			Arch:          "amd64",
			ClientVersion: "0.1.0",
		},
	}); err != nil {
		t.Fatalf("send register: %v", err)
	}

	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatalf("expected connection close for non-existent join token")
	}

	if hub.IsDeviceOnline("dev-fake") {
		t.Fatalf("device with fake join token should not be online")
	}
}

func TestHubStaticTokenStillWorksWithJoinTokenStore(t *testing.T) {
	hub, _, _, _, deviceWSURL := testHubServerWithJoinTokens(t)

	// Register with valid static device token
	conn := registerDevice(t, deviceWSURL, protocol.RegisterPayload{
		DeviceID:      "dev-static",
		Hostname:      "host-static",
		Token:         "device-token",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	})
	defer conn.Close()

	waitForCondition(t, time.Second, func() bool {
		return hub.IsDeviceOnline("dev-static")
	}, "device with static token should be online")
}

func TestHubDisconnectDevice(t *testing.T) {
	hub, _, _, _, deviceWSURL := testHubServerWithJoinTokens(t)

	conn := registerDevice(t, deviceWSURL, protocol.RegisterPayload{
		DeviceID:      "dev-disconnect",
		Hostname:      "host-disconnect",
		Token:         "device-token",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	})
	defer conn.Close()

	waitForCondition(t, time.Second, func() bool {
		return hub.IsDeviceOnline("dev-disconnect")
	}, "device should be online before disconnect")

	hub.DisconnectDevice("dev-disconnect")

	waitForCondition(t, 2*time.Second, func() bool {
		return !hub.IsDeviceOnline("dev-disconnect")
	}, "device should be offline after DisconnectDevice")
}

func TestHubDisconnectNonExistentDevice(t *testing.T) {
	hub, _, _, _, _ := testHubServerWithJoinTokens(t)

	// Should not panic
	hub.DisconnectDevice("non-existent-device")
}

func TestHubRejectsEmptyDeviceID(t *testing.T) {
	_, _, _, _, deviceWSURL := testHubServerWithJoinTokens(t)

	conn, _, err := websocket.DefaultDialer.Dial(deviceWSURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(protocol.Envelope{
		Type: protocol.TypeRegister,
		Data: protocol.RegisterPayload{
			DeviceID:      "",
			Hostname:      "host",
			Token:         "device-token",
			OS:            "linux",
			Arch:          "amd64",
			ClientVersion: "0.1.0",
		},
	}); err != nil {
		t.Fatalf("send register: %v", err)
	}

	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatalf("expected connection close for empty device ID")
	}
}

func TestHubRejectsNonRegisterFirstMessage(t *testing.T) {
	_, _, _, _, deviceWSURL := testHubServerWithJoinTokens(t)

	conn, _, err := websocket.DefaultDialer.Dial(deviceWSURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	// Send a heartbeat instead of register
	if err := conn.WriteJSON(protocol.Envelope{
		Type: protocol.TypeHeartbeat,
		Data: protocol.HeartbeatPayload{
			DeviceID: "dev-1",
		},
	}); err != nil {
		t.Fatalf("send heartbeat: %v", err)
	}

	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatalf("expected connection close for non-register first message")
	}
}

func TestHubJoinTokenMarksDeviceAfterUse(t *testing.T) {
	_, joinTokens, _, _, deviceWSURL := testHubServerWithJoinTokens(t)

	tok, err := joinTokens.CreateToken("track-usage", time.Hour, "")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	conn := registerDevice(t, deviceWSURL, protocol.RegisterPayload{
		DeviceID:      "dev-track",
		Hostname:      "host-track",
		Token:         tok.ID,
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	})
	defer conn.Close()

	// Verify the token is now consumed
	listed, err := joinTokens.ListTokens()
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 token, got %d", len(listed))
	}
	if listed[0].UsedAt == nil {
		t.Fatalf("expected usedAt to be set after WS registration")
	}
	if listed[0].UsedByDevice == nil || *listed[0].UsedByDevice != "dev-track" {
		t.Fatalf("expected usedByDevice='dev-track', got %+v", listed[0].UsedByDevice)
	}
}

func TestHubDeviceReconnect(t *testing.T) {
	hub, _, _, _, deviceWSURL := testHubServerWithJoinTokens(t)

	// Connect first time
	conn1 := registerDevice(t, deviceWSURL, protocol.RegisterPayload{
		DeviceID:      "dev-reconnect",
		Hostname:      "host-1",
		Token:         "device-token",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	})

	waitForCondition(t, time.Second, func() bool {
		return hub.IsDeviceOnline("dev-reconnect")
	}, "device should be online")

	// Reconnect same device (overwrites session)
	conn2 := registerDevice(t, deviceWSURL, protocol.RegisterPayload{
		DeviceID:      "dev-reconnect",
		Hostname:      "host-2",
		Token:         "device-token",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.2.0",
	})
	defer conn2.Close()

	waitForCondition(t, time.Second, func() bool {
		return hub.IsDeviceOnline("dev-reconnect")
	}, "device should remain online after reconnect")

	// Old connection should be closed
	_ = conn1.Close()
}

func TestHubJanitorStartStop(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema: %v", err)
	}
	commands := NewCommandStore(db)
	if err := commands.EnsureSchema(); err != nil {
		t.Fatalf("ensure command schema: %v", err)
	}
	auth := NewTokenAuth("device", []byte("hub-test-secret"), users)
	hub := NewHub(devices, commands, nil, users, auth)

	// Start and stop should not panic
	hub.Start()
	hub.Start() // double start is safe
	hub.Stop()
	hub.Stop() // double stop is safe
}

func TestTokenHashPrefix(t *testing.T) {
	prefix := tokenHashPrefix("device-token-secret")
	if prefix == "" || prefix == "device-token-secret" {
		t.Fatalf("expected hashed prefix, got %q", prefix)
	}
	if len(prefix) != 12 {
		t.Fatalf("expected 12-char prefix, got %d", len(prefix))
	}
	if tokenHashPrefix("   ") != "none" {
		t.Fatalf("expected empty token prefix to be 'none'")
	}
}
