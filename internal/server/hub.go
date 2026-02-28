package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/raystone-ai/clawmini/internal/protocol"
)

const (
	wsReadLimit                = 2 << 20
	wsWriteWait                = 10 * time.Second
	wsPingInterval             = 30 * time.Second
	wsPongWait                 = 70 * time.Second
	commandExpirySweepInterval = 60 * time.Second
	commandExpiryGraceSeconds  = 30
)

type deviceSession struct {
	deviceID string
	conn     *websocket.Conn
	mu       sync.Mutex
}

type browserSession struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (s *deviceSession) writeJSON(v interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
	return s.conn.WriteJSON(v)
}

func (s *deviceSession) writePing() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(wsWriteWait))
}

func (s *browserSession) writeJSON(v interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
	return s.conn.WriteJSON(v)
}

func (s *browserSession) writePing() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(wsWriteWait))
}

type Hub struct {
	devices    *DeviceStore
	commands   *CommandStore
	joinTokens *JoinTokenStore
	auth       *TokenAuth

	upgrader websocket.Upgrader

	mu       sync.RWMutex
	deviceWS map[string]*deviceSession
	browsers map[*browserSession]struct{}

	janitorMu      sync.Mutex
	janitorRunning bool
	janitorStopCh  chan struct{}
	janitorDoneCh  chan struct{}
}

func NewHub(devices *DeviceStore, commands *CommandStore, joinTokens *JoinTokenStore, auth *TokenAuth) *Hub {
	return &Hub{
		devices:    devices,
		commands:   commands,
		joinTokens: joinTokens,
		auth:       auth,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     validateWSOrigin,
		},
		deviceWS: make(map[string]*deviceSession),
		browsers: make(map[*browserSession]struct{}),
	}
}

func (h *Hub) Start() {
	h.janitorMu.Lock()
	if h.janitorRunning {
		h.janitorMu.Unlock()
		return
	}
	h.janitorRunning = true
	h.janitorStopCh = make(chan struct{})
	h.janitorDoneCh = make(chan struct{})
	stopCh := h.janitorStopCh
	doneCh := h.janitorDoneCh
	h.janitorMu.Unlock()

	go h.runCommandExpiryJanitor(stopCh, doneCh)
}

func (h *Hub) Stop() {
	h.janitorMu.Lock()
	if !h.janitorRunning {
		h.janitorMu.Unlock()
		return
	}
	stopCh := h.janitorStopCh
	doneCh := h.janitorDoneCh
	h.janitorRunning = false
	h.janitorStopCh = nil
	h.janitorDoneCh = nil
	h.janitorMu.Unlock()

	close(stopCh)
	<-doneCh
}

func (h *Hub) runCommandExpiryJanitor(stopCh <-chan struct{}, doneCh chan<- struct{}) {
	defer close(doneCh)
	ticker := time.NewTicker(commandExpirySweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			expired, err := h.commands.FailExpiredSent(commandExpiryGraceSeconds)
			if err != nil {
				log.Printf("command expiry janitor error: %v", err)
				continue
			}
			if expired > 0 {
				log.Printf("command expiry janitor marked %d command(s) as timeout", expired)
			}
		}
	}
}

func configureWSReadKeepalive(conn *websocket.Conn) {
	conn.SetReadLimit(wsReadLimit)
	_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})
	conn.SetPingHandler(func(appData string) error {
		_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(wsWriteWait))
	})
}

func startPingLoop(sendPing func() error, onError func()) chan struct{} {
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(wsPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if err := sendPing(); err != nil {
					onError()
					return
				}
			}
		}
	}()
	return stop
}

func (h *Hub) HandleDeviceWS(w http.ResponseWriter, r *http.Request) {
	log.Printf("[ws] device connect from %s", r.RemoteAddr)
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	log.Printf("[ws] upgrade OK for %s", r.RemoteAddr)
	h.serveDevice(conn)
}

func (h *Hub) HandleBrowserWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(wsReadLimit)

	if err := h.authenticateBrowser(conn); err != nil {
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "unauthorized"), time.Now().Add(time.Second))
		return
	}

	sess := &browserSession{conn: conn}
	configureWSReadKeepalive(conn)
	stopPing := startPingLoop(sess.writePing, func() { _ = conn.Close() })
	defer close(stopPing)

	h.addBrowser(sess)
	defer h.removeBrowser(sess)

	if devices, err := h.devices.ListDevices(); err == nil {
		_ = sess.writeJSON(map[string]interface{}{
			"event": "snapshot",
			"ts":    nowUnix(),
			"data":  devices,
		})
	}

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (h *Hub) serveDevice(conn *websocket.Conn) {
	defer conn.Close()
	conn.SetReadLimit(wsReadLimit)

	_, payload, err := conn.ReadMessage()
	if err != nil {
		return
	}

	var env protocol.RawEnvelope
	if err := json.Unmarshal(payload, &env); err != nil || env.Type != protocol.TypeRegister {
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseUnsupportedData, "register required"), time.Now().Add(time.Second))
		return
	}

	var reg protocol.RegisterPayload
	if err := json.Unmarshal(env.Data, &reg); err != nil {
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseUnsupportedData, "bad register payload"), time.Now().Add(time.Second))
		return
	}
	log.Printf("[ws] register: deviceID=%s", reg.DeviceID)
	if reg.DeviceID == "" {
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "invalid token"), time.Now().Add(time.Second))
		return
	}
	if !h.auth.ValidateDeviceToken(reg.Token) {
		if h.joinTokens == nil || h.joinTokens.ValidateAndConsume(reg.Token, reg.DeviceID) != nil {
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "invalid token"), time.Now().Add(time.Second))
			return
		}
	}
	if err := h.devices.UpsertDevice(reg); err != nil {
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "db error"), time.Now().Add(time.Second))
		return
	}

	sess := &deviceSession{deviceID: reg.DeviceID, conn: conn}
	configureWSReadKeepalive(conn)
	stopPing := startPingLoop(sess.writePing, func() { _ = conn.Close() })
	defer close(stopPing)

	h.addDevice(sess)
	defer h.removeDevice(sess)

	_ = sess.writeJSON(protocol.Envelope{Type: protocol.TypeAck, ID: reg.DeviceID, Data: map[string]string{"status": "registered"}})
	if snap, err := h.devices.GetDevice(reg.DeviceID); err == nil {
		h.broadcast("device_connected", snap)
	}

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if snap, snapErr := h.devices.GetDevice(reg.DeviceID); snapErr == nil {
				h.broadcast("device_disconnected", snap)
			}
			return
		}
		h.handleDeviceMessage(sess, data)
	}
}

func (h *Hub) authenticateBrowser(conn *websocket.Conn) error {
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Time{})

	var env protocol.RawEnvelope
	if err := json.Unmarshal(payload, &env); err != nil || env.Type != "auth" {
		return errors.New("auth required")
	}

	var authPayload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(env.Data, &authPayload); err != nil {
		return err
	}
	if !h.auth.ValidateAdminToken(authPayload.Token) {
		return errors.New("invalid token")
	}

	return nil
}

func validateWSOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return r.URL.Path == "/ws"
	}

	originURL, err := url.Parse(origin)
	if err != nil || originURL.Host == "" {
		return false
	}
	return strings.EqualFold(originURL.Host, r.Host)
}

func (h *Hub) handleDeviceMessage(sess *deviceSession, data []byte) {
	var env protocol.RawEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return
	}
	switch env.Type {
	case protocol.TypeHeartbeat:
		var hb protocol.HeartbeatPayload
		if err := json.Unmarshal(env.Data, &hb); err != nil {
			return
		}
		if hb.DeviceID == "" {
			hb.DeviceID = sess.deviceID
		}
		if hb.DeviceID != sess.deviceID {
			return
		}
		if err := h.devices.UpdateHeartbeat(hb); err != nil {
			return
		}
		if snap, err := h.devices.GetDevice(sess.deviceID); err == nil {
			h.broadcast("device_heartbeat", snap)
		}
	case protocol.TypeResult:
		var result protocol.ResultPayload
		if err := json.Unmarshal(env.Data, &result); err != nil {
			return
		}
		if result.CommandID == "" {
			return
		}
		if err := h.commands.Complete(sess.deviceID, result); err != nil {
			return
		}
		if rec, err := h.commands.GetByDeviceAndID(sess.deviceID, result.CommandID); err == nil {
			h.broadcast("command_result", rec)
		}
	}
}

func (h *Hub) DispatchCommand(deviceID string, cmd protocol.CommandPayload) error {
	sess := h.getDevice(deviceID)
	if sess == nil {
		return errors.New("device offline")
	}
	if err := sess.writeJSON(protocol.Envelope{Type: protocol.TypeCommand, ID: cmd.CommandID, Data: cmd}); err != nil {
		h.removeDevice(sess)
		return err
	}
	if err := h.commands.MarkSent(cmd.CommandID); err != nil {
		return err
	}
	cmdForBroadcast := cmd
	cmdForBroadcast.Args = redactSensitiveArgs(cmd.Command, cmd.Args)
	h.broadcast("command_dispatched", cmdForBroadcast)
	return nil
}

func (h *Hub) IsDeviceOnline(deviceID string) bool {
	return h.getDevice(deviceID) != nil
}

func (h *Hub) DisconnectDevice(deviceID string) {
	sess := h.getDevice(deviceID)
	if sess == nil {
		return
	}
	h.removeDevice(sess)
	_ = sess.conn.Close()
}

func (h *Hub) addDevice(sess *deviceSession) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if existing, ok := h.deviceWS[sess.deviceID]; ok && existing != sess {
		_ = existing.conn.Close()
	}
	h.deviceWS[sess.deviceID] = sess
}

func (h *Hub) removeDevice(sess *deviceSession) {
	h.mu.Lock()
	defer h.mu.Unlock()
	current, ok := h.deviceWS[sess.deviceID]
	if ok && current == sess {
		delete(h.deviceWS, sess.deviceID)
	}
}

func (h *Hub) getDevice(deviceID string) *deviceSession {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.deviceWS[deviceID]
}

func (h *Hub) addBrowser(sess *browserSession) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.browsers[sess] = struct{}{}
}

func (h *Hub) removeBrowser(sess *browserSession) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.browsers, sess)
}

func (h *Hub) broadcast(event string, data interface{}) {
	h.mu.RLock()
	listeners := make([]*browserSession, 0, len(h.browsers))
	for sess := range h.browsers {
		listeners = append(listeners, sess)
	}
	h.mu.RUnlock()

	message := map[string]interface{}{
		"event": event,
		"ts":    nowUnix(),
		"data":  data,
	}
	for _, sess := range listeners {
		if err := sess.writeJSON(message); err != nil {
			_ = sess.conn.Close()
			h.removeBrowser(sess)
		}
	}
}
