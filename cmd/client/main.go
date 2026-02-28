package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"context"

	iclient "github.com/raystone-ai/clawmini/internal/client"
)

const defaultClientVersion = "0.2.1"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "join":
		if err := runJoin(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Println("Usage: clawmini-client join --server ws://host:8080/ws --token <device-token>")
}

func runJoin(args []string) error {
	fs := flag.NewFlagSet("join", flag.ContinueOnError)
	server := fs.String("server", "ws://localhost:8080/ws", "ClawMini server URL")
	token := fs.String("token", "", "Device registration token")
	deviceID := fs.String("device-id", defaultDeviceID(), "Device ID")
	hostname := fs.String("hostname", defaultHostname(), "Device hostname")
	clientVersion := fs.String("client-version", defaultClientVersion, "Client version")
	openclawVersion := fs.String("openclaw-version", "", "Known OpenClaw version override")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*token) == "" {
		return fmt.Errorf("--token is required")
	}

	wsURL, err := normalizeServerURL(*server)
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	collector := iclient.NewCollector()
	executor := iclient.NewExecutor()
	conn := iclient.NewConnection(iclient.ConnectionConfig{
		ServerURL:       wsURL,
		Token:           *token,
		DeviceID:        *deviceID,
		Hostname:        *hostname,
		ClientVersion:   *clientVersion,
		OpenClawVersion: *openclawVersion,
	}, collector, executor)

	log.Printf("clawmini-client v%s", defaultClientVersion)
	log.Printf("joining server as %s (%s)", *deviceID, *hostname)
	return conn.Run(ctx)
}

func normalizeServerURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("server URL is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "ws://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid server URL: %w", err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/ws"
	}
	return u.String(), nil
}

func defaultHostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown"
	}
	return h
}

func defaultDeviceID() string {
	if b, err := os.ReadFile("/etc/machine-id"); err == nil {
		id := strings.TrimSpace(string(b))
		if id != "" {
			return id
		}
	}
	h := defaultHostname()
	randPart := make([]byte, 4)
	_, _ = rand.Read(randPart)
	return fmt.Sprintf("%s-%s", h, hex.EncodeToString(randPart))
}
