package protocol

import "encoding/json"

// RawEnvelope decodes WS messages before unmarshalling payload by message type.
type RawEnvelope struct {
	Type string          `json:"type"`
	ID   string          `json:"id,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}
