package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/raystone-ai/clawmini/internal/protocol"
)

type ConnectionConfig struct {
	ServerURL       string
	Token           string
	DeviceID        string
	Hostname        string
	ClientVersion   string
	OpenClawVersion string
}

type Connection struct {
	cfg       ConnectionConfig
	collector *Collector
	executor  *Executor
	dialer    websocket.Dialer
}

func NewConnection(cfg ConnectionConfig, collector *Collector, executor *Executor) *Connection {
	return &Connection{
		cfg:       cfg,
		collector: collector,
		executor:  executor,
		dialer:    websocket.Dialer{HandshakeTimeout: 15 * time.Second},
	}
}

func (c *Connection) Run(ctx context.Context) error {
	backoff := time.Second
	for {
		err := c.runOnce(ctx)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return nil
		}
		log.Printf("connection lost: %v, reconnecting...", err)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

func (c *Connection) runOnce(ctx context.Context) error {
	log.Printf("connecting to %s", c.cfg.ServerURL)
	conn, _, err := c.dialer.DialContext(ctx, c.cfg.ServerURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	var writeMu sync.Mutex
	send := func(v interface{}) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(v)
	}

	register := protocol.RegisterPayload{
		DeviceID:        c.cfg.DeviceID,
		Hostname:        c.cfg.Hostname,
		Token:           c.cfg.Token,
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		OpenClawVersion: c.cfg.OpenClawVersion,
		ClientVersion:   c.cfg.ClientVersion,
	}
	if err := send(protocol.Envelope{Type: protocol.TypeRegister, ID: c.cfg.DeviceID, Data: register}); err != nil {
		return fmt.Errorf("register failed: %w", err)

	}
	log.Printf("connected and registered as %s", c.cfg.DeviceID)

	cmdCh := make(chan protocol.CommandPayload)
	errCh := make(chan error, 1)
	go func() {
		defer close(cmdCh)
		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			var env protocol.RawEnvelope
			if err := json.Unmarshal(payload, &env); err != nil {
				continue
			}
			if env.Type != protocol.TypeCommand {
				continue
			}
			var cmd protocol.CommandPayload
			if err := json.Unmarshal(env.Data, &cmd); err != nil {
				continue
			}
			if cmd.CommandID == "" {
				cmd.CommandID = env.ID
			}
			select {
			case <-ctx.Done():
				return
			case cmdCh <- cmd:
			}
		}
	}()

	heartbeatTicker := time.NewTicker(30 * time.Second)
	defer heartbeatTicker.Stop()
	if err := c.sendHeartbeat(ctx, send); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return context.Canceled
		case err := <-errCh:
			if err == nil {
				return errors.New("connection closed")
			}
			return err
		case <-heartbeatTicker.C:
			if err := c.sendHeartbeat(ctx, send); err != nil {
				return err
			}
		case cmd, ok := <-cmdCh:
			if !ok {
				return errors.New("command channel closed")
			}
			result := c.executor.Execute(ctx, cmd)
			if err := send(protocol.Envelope{Type: protocol.TypeResult, ID: cmd.CommandID, Data: result}); err != nil {
				return err
			}
		}
	}
}

func (c *Connection) sendHeartbeat(ctx context.Context, send func(interface{}) error) error {
	collectCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	hb := c.collector.Collect(collectCtx, c.cfg.DeviceID)
	return send(protocol.Envelope{Type: protocol.TypeHeartbeat, ID: c.cfg.DeviceID, Data: hb})
}
