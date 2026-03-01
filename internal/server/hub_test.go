package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/raystone-ai/clawmini/internal/protocol"
)

func testHubServer(t *testing.T) (*Hub, *CommandStore, string, string, string) {
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
	auth := NewTokenAuth("device-token", []byte("hub-test-secret"), users)
	adminUser, err := users.Authenticate("admin", "admin")
	if err != nil {
		t.Fatalf("authenticate default admin: %v", err)
	}
	adminToken, err := auth.GenerateToken(adminUser)
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}
	hub := NewHub(devices, commands, nil, users, auth)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.HandleDeviceWS)
	mux.HandleFunc("/api/ws", hub.HandleBrowserWS)
	baseHTTPURL := startTCP4HTTPServer(t, mux)

	base := "ws" + strings.TrimPrefix(baseHTTPURL, "http")
	return hub, commands, base + "/ws", base + "/api/ws", adminToken
}

func registerDevice(t *testing.T, wsURL string, reg protocol.RegisterPayload) *websocket.Conn {
	t.Helper()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial device ws: %v", err)
	}

	if err := conn.WriteJSON(protocol.Envelope{
		Type: protocol.TypeRegister,
		Data: reg,
	}); err != nil {
		_ = conn.Close()
		t.Fatalf("send register: %v", err)
	}

	var ack protocol.Envelope
	if err := conn.ReadJSON(&ack); err != nil {
		_ = conn.Close()
		t.Fatalf("read register ack: %v", err)
	}
	if ack.Type != protocol.TypeAck {
		_ = conn.Close()
		t.Fatalf("expected ack, got %q", ack.Type)
	}

	return conn
}

func registerBrowser(t *testing.T, wsURL, token string) *websocket.Conn {
	t.Helper()

	u, err := url.Parse(wsURL)
	if err != nil {
		t.Fatalf("parse browser ws url: %v", err)
	}
	originScheme := "http"
	if u.Scheme == "wss" {
		originScheme = "https"
	}
	headers := http.Header{}
	headers.Set("Origin", originScheme+"://"+u.Host)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("dial browser ws: %v", err)
	}
	if err := conn.WriteJSON(map[string]interface{}{
		"type": "auth",
		"data": map[string]string{
			"token": token,
		},
	}); err != nil {
		_ = conn.Close()
		t.Fatalf("send browser auth: %v", err)
	}

	return conn
}

func TestHubDeviceConnectionLifecycle(t *testing.T) {
	hub, _, deviceWSURL, _, _ := testHubServer(t)

	conn := registerDevice(t, deviceWSURL, protocol.RegisterPayload{
		DeviceID:      "dev-1",
		Hostname:      "host-a",
		Token:         "device-token",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	})

	waitForCondition(t, time.Second, func() bool {
		return hub.IsDeviceOnline("dev-1")
	}, "device should be online after registration")

	_ = conn.Close()

	waitForCondition(t, 2*time.Second, func() bool {
		return !hub.IsDeviceOnline("dev-1")
	}, "device should be removed after disconnect")
}

func TestHubRejectsInvalidDeviceToken(t *testing.T) {
	hub, _, deviceWSURL, _, _ := testHubServer(t)

	conn, _, err := websocket.DefaultDialer.Dial(deviceWSURL, nil)
	if err != nil {
		t.Fatalf("dial device ws: %v", err)
	}
	defer conn.Close()

	err = conn.WriteJSON(protocol.Envelope{
		Type: protocol.TypeRegister,
		Data: protocol.RegisterPayload{
			DeviceID:      "dev-bad",
			Hostname:      "host-b",
			Token:         "bad-token",
			OS:            "linux",
			Arch:          "amd64",
			ClientVersion: "0.1.0",
		},
	})
	if err != nil {
		t.Fatalf("send register: %v", err)
	}

	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatalf("expected connection close for invalid token")
	}

	if hub.IsDeviceOnline("dev-bad") {
		t.Fatalf("invalid token device should never be online")
	}
}

func TestHubDispatchCommandToOnlineDevice(t *testing.T) {
	hub, commands, deviceWSURL, _, _ := testHubServer(t)

	conn := registerDevice(t, deviceWSURL, protocol.RegisterPayload{
		DeviceID:      "dev-1",
		Hostname:      "host-a",
		Token:         "device-token",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	})
	defer conn.Close()

	waitForCondition(t, time.Second, func() bool {
		return hub.IsDeviceOnline("dev-1")
	}, "device should be online before dispatch")

	rec, err := commands.Create("dev-1", "openclaw", []string{"status", "--json"}, 30)
	if err != nil {
		t.Fatalf("create command: %v", err)
	}

	msg := protocol.CommandPayload{
		CommandID: rec.ID,
		Command:   rec.Command,
		Args:      rec.Args,
		Timeout:   rec.Timeout,
	}
	if err := hub.DispatchCommand("dev-1", msg); err != nil {
		t.Fatalf("dispatch command: %v", err)
	}

	var env protocol.Envelope
	if err := conn.ReadJSON(&env); err != nil {
		t.Fatalf("read command envelope: %v", err)
	}
	if env.Type != protocol.TypeCommand || env.ID != rec.ID {
		t.Fatalf("unexpected command envelope: %+v", env)
	}

	got, err := commands.GetByDeviceAndID("dev-1", rec.ID)
	if err != nil {
		t.Fatalf("get command after dispatch: %v", err)
	}
	if got.Status != "sent" {
		t.Fatalf("expected sent status after dispatch, got %q", got.Status)
	}
}

func TestHubBrowserConnectionLifecycle(t *testing.T) {
	hub, _, _, browserWSURL, adminToken := testHubServer(t)

	conn := registerBrowser(t, browserWSURL, adminToken)

	var msg map[string]interface{}
	if err := conn.ReadJSON(&msg); err != nil {
		_ = conn.Close()
		t.Fatalf("read browser snapshot: %v", err)
	}
	if msg["event"] != "snapshot" {
		_ = conn.Close()
		t.Fatalf("expected snapshot event, got %#v", msg["event"])
	}

	_ = conn.Close()

	waitForCondition(t, 2*time.Second, func() bool {
		hub.mu.RLock()
		defer hub.mu.RUnlock()
		return len(hub.browsers) == 0
	}, "browser session should be removed after disconnect")
}

func TestHubRejectsBrowserWithoutValidAuthMessage(t *testing.T) {
	_, _, _, browserWSURL, _ := testHubServer(t)

	conn := registerBrowser(t, browserWSURL, "bad-token")
	defer conn.Close()

	_, _, err := conn.ReadMessage()
	if err == nil {
		t.Fatalf("expected browser ws close for invalid token")
	}
}

func TestHubAcceptsJoinTokenOnce(t *testing.T) {
	db := openTestDB(t)

	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	commands := NewCommandStore(db)
	if err := commands.EnsureSchema(); err != nil {
		t.Fatalf("ensure command schema: %v", err)
	}
	joinTokens := NewJoinTokenStore(db)
	if err := joinTokens.EnsureSchema(); err != nil {
		t.Fatalf("ensure join token schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema: %v", err)
	}
	auth := NewTokenAuth("device-token", []byte("hub-test-secret"), users)
	hub := NewHub(devices, commands, joinTokens, users, auth)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.HandleDeviceWS)
	baseHTTPURL := startTCP4HTTPServer(t, mux)
	deviceWSURL := "ws" + strings.TrimPrefix(baseHTTPURL, "http") + "/ws"

	token, err := joinTokens.CreateToken("一次性令牌", time.Hour, "")
	if err != nil {
		t.Fatalf("create join token: %v", err)
	}

	conn := registerDevice(t, deviceWSURL, protocol.RegisterPayload{
		DeviceID:      "dev-join-1",
		Hostname:      "host-join",
		Token:         token.ID,
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	})
	defer conn.Close()

	waitForCondition(t, time.Second, func() bool {
		return hub.IsDeviceOnline("dev-join-1")
	}, "join token device should be online after registration")

	reuseConn, _, err := websocket.DefaultDialer.Dial(deviceWSURL, nil)
	if err != nil {
		t.Fatalf("dial device ws for reused token: %v", err)
	}
	defer reuseConn.Close()

	if err := reuseConn.WriteJSON(protocol.Envelope{
		Type: protocol.TypeRegister,
		Data: protocol.RegisterPayload{
			DeviceID:      "dev-join-2",
			Hostname:      "host-join-2",
			Token:         token.ID,
			OS:            "linux",
			Arch:          "amd64",
			ClientVersion: "0.1.0",
		},
	}); err != nil {
		t.Fatalf("send reused join token register: %v", err)
	}

	_, _, err = reuseConn.ReadMessage()
	if err == nil {
		t.Fatalf("expected connection close for reused join token")
	}
}

func TestHubRejectsBrowserMismatchedOrigin(t *testing.T) {
	_, _, _, browserWSURL, _ := testHubServer(t)

	headers := http.Header{}
	headers.Set("Origin", "https://evil.example.com")
	conn, resp, err := websocket.DefaultDialer.Dial(browserWSURL, headers)
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil {
		t.Fatalf("expected websocket dial to fail for mismatched origin")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		got := 0
		if resp != nil {
			got = resp.StatusCode
		}
		t.Fatalf("expected 403 for mismatched origin, got %d", got)
	}
}

func TestHubAllowedOriginsList(t *testing.T) {
	hub, _, _, browserWSURL, adminToken := testHubServer(t)
	if err := hub.SetAllowedOrigins([]string{"https://console.example.com"}); err != nil {
		t.Fatalf("set allowed origins: %v", err)
	}

	headers := http.Header{}
	headers.Set("Origin", "https://console.example.com")
	conn, _, err := websocket.DefaultDialer.Dial(browserWSURL, headers)
	if err != nil {
		t.Fatalf("dial browser ws: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]interface{}{
		"type": "auth",
		"data": map[string]string{
			"token": adminToken,
		},
	}); err != nil {
		t.Fatalf("send browser auth: %v", err)
	}

	var msg map[string]interface{}
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read browser snapshot: %v", err)
	}
	if msg["event"] != "snapshot" {
		t.Fatalf("expected snapshot event, got %#v", msg["event"])
	}
}

func TestHubAllowedOriginsStillAllowsDeviceWithoutOrigin(t *testing.T) {
	hub, _, deviceWSURL, _, _ := testHubServer(t)
	if err := hub.SetAllowedOrigins([]string{"https://console.example.com"}); err != nil {
		t.Fatalf("set allowed origins: %v", err)
	}

	conn := registerDevice(t, deviceWSURL, protocol.RegisterPayload{
		DeviceID:      "dev-originless",
		Hostname:      "host-originless",
		Token:         "device-token",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	})
	defer conn.Close()

	waitForCondition(t, time.Second, func() bool {
		return hub.IsDeviceOnline("dev-originless")
	}, "device should connect without origin header")
}

func TestHubSetAllowedOriginsRejectsInvalidOrigin(t *testing.T) {
	hub, _, _, _, _ := testHubServer(t)
	if err := hub.SetAllowedOrigins([]string{"not-a-url"}); err == nil {
		t.Fatalf("expected invalid origin to fail")
	}
}
