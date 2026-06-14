package wsbrowser

import (
	"encoding/json"
	"testing"
)

func TestResizeMsg_Parse(t *testing.T) {
	data := []byte(`{"type":"resize","cols":120,"rows":40}`)
	var msg resizeMsg
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if msg.Type != "resize" {
		t.Errorf("Type: got %q, want resize", msg.Type)
	}
	if msg.Cols != 120 {
		t.Errorf("Cols: got %d, want 120", msg.Cols)
	}
	if msg.Rows != 40 {
		t.Errorf("Rows: got %d, want 40", msg.Rows)
	}
}

func TestResizeMsg_InvalidJSON(t *testing.T) {
	data := []byte(`not json`)
	var msg resizeMsg
	if err := json.Unmarshal(data, &msg); err == nil {
		t.Error("expected parse error for invalid JSON")
	}
}

func TestResizeMsg_WrongType(t *testing.T) {
	data := []byte(`{"type":"input","cols":80,"rows":24}`)
	var msg resizeMsg
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if msg.Type == "resize" {
		t.Error("type should not be resize")
	}
}
