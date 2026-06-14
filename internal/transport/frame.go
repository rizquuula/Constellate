package transport

import "encoding/json"

// Frame holds one decoded control line from the NDJSON stream.
type Frame struct {
	Type MessageType
	Raw  json.RawMessage
}

// typeHeader is used to peek at the "type" field of an incoming message.
type typeHeader struct {
	Type MessageType `json:"type"`
}
