package protocol

// Message types for Client <-> Server communication
const (
	TypeRegister  = "register"
	TypeHeartbeat = "heartbeat"
	TypeCommand   = "command"
	TypeResult    = "result"
	TypeAck       = "ack"
)

// Envelope wraps all WSS messages
type Envelope struct {
	Type string      `json:"type"`
	ID   string      `json:"id,omitempty"`
	Data interface{} `json:"data,omitempty"`
}

// RegisterPayload sent by Client on first connect
type RegisterPayload struct {
	DeviceID        string `json:"deviceId"`
	Hostname        string `json:"hostname"`
	Token           string `json:"token"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	HasOpenClaw     bool   `json:"has_openclaw"`
	OpenClawVersion string `json:"openclaw_version,omitempty"`
	ClientVersion   string `json:"clientVersion"`
}

// HeartbeatPayload sent by Client every 30s
type HeartbeatPayload struct {
	DeviceID string       `json:"deviceId"`
	System   SystemInfo   `json:"system"`
	OpenClaw OpenClawInfo `json:"openclaw"`
}

type SystemInfo struct {
	Username  string  `json:"username"`
	CPUUsage  float64 `json:"cpuUsage"`
	MemTotal  uint64  `json:"memTotal"`
	MemUsed   uint64  `json:"memUsed"`
	DiskTotal uint64  `json:"diskTotal"`
	DiskUsed  uint64  `json:"diskUsed"`
	Uptime    uint64  `json:"uptime"`
}

type OpenClawInfo struct {
	Installed       bool          `json:"installed"`
	Version         string        `json:"version"`
	GatewayStatus   string        `json:"gatewayStatus"`
	UpdateAvailable string        `json:"updateAvailable,omitempty"`
	Channels        []ChannelInfo `json:"channels"`
}

type ChannelInfo struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Messages int    `json:"messages,omitempty"`
	Error    string `json:"error,omitempty"`
}

// CommandPayload sent by Server to Client
type CommandPayload struct {
	CommandID string   `json:"commandId"`
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	Timeout   int      `json:"timeout"`
}

// ResultPayload sent by Client back to Server
type ResultPayload struct {
	CommandID  string `json:"commandId"`
	ExitCode   int    `json:"exitCode"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"durationMs"`
}
