package transport_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/rizquuula/Constellate/internal/transport"
)

func TestAgentToken_RoundTrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	machineID := "01JTEST123"
	now := int64(1700000000)

	token := transport.BuildAgentToken(machineID, priv, now)

	gotMachineID, gotTs, gotSig, err := transport.ParseAgentToken(token)
	if err != nil {
		t.Fatalf("ParseAgentToken: %v", err)
	}
	if gotMachineID != machineID {
		t.Errorf("machineID: got %q, want %q", gotMachineID, machineID)
	}
	if gotTs != now {
		t.Errorf("unixTs: got %d, want %d", gotTs, now)
	}

	if err := transport.VerifyAgentToken(pub, gotMachineID, gotTs, gotSig, now, 120); err != nil {
		t.Fatalf("VerifyAgentToken: %v", err)
	}
}

func TestAgentToken_WrongKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	wrongPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey wrong: %v", err)
	}

	token := transport.BuildAgentToken("machine1", priv, 1700000000)
	_, ts, sig, _ := transport.ParseAgentToken(token)

	err = transport.VerifyAgentToken(wrongPub, "machine1", ts, sig, 1700000000, 120)
	if !errors.Is(err, transport.ErrInvalidAgentToken) {
		t.Errorf("expected ErrInvalidAgentToken, got %v", err)
	}
}

func TestAgentToken_TamperedMachineID(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	token := transport.BuildAgentToken("machine1", priv, 1700000000)
	_, ts, sig, _ := transport.ParseAgentToken(token)

	// Verify with a different machineID should fail (sig covers original ID).
	err = transport.VerifyAgentToken(pub, "evil-machine", ts, sig, 1700000000, 120)
	if !errors.Is(err, transport.ErrInvalidAgentToken) {
		t.Errorf("expected ErrInvalidAgentToken for tampered machineID, got %v", err)
	}
}

func TestAgentToken_TamperedTimestamp(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	token := transport.BuildAgentToken("machine1", priv, 1700000000)
	_, _, sig, _ := transport.ParseAgentToken(token)

	// Verify with a different ts should fail.
	err = transport.VerifyAgentToken(pub, "machine1", 1700000001, sig, 1700000001, 120)
	if !errors.Is(err, transport.ErrInvalidAgentToken) {
		t.Errorf("expected ErrInvalidAgentToken for tampered ts, got %v", err)
	}
}

func TestAgentToken_StaleTsBeyondSkew(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	ts := int64(1700000000)
	token := transport.BuildAgentToken("machine1", priv, ts)
	_, gotTs, gotSig, _ := transport.ParseAgentToken(token)

	// now = ts + 121 exceeds skew of 120.
	err = transport.VerifyAgentToken(pub, "machine1", gotTs, gotSig, ts+121, 120)
	if !errors.Is(err, transport.ErrInvalidAgentToken) {
		t.Errorf("expected ErrInvalidAgentToken for stale ts, got %v", err)
	}
}

func TestParseAgentToken_Malformed(t *testing.T) {
	cases := []string{
		"",
		"v1.machine1.1700000000",    // only 3 parts
		"v2.machine1.1700000000.xx", // wrong version
		"v1..1700000000.xx",         // empty machineID
		"v1.m.notanint.xx",          // bad ts
		"v1.m.1700000000.!!!",       // bad base64
	}
	for _, tc := range cases {
		_, _, _, err := transport.ParseAgentToken(tc)
		if !errors.Is(err, transport.ErrInvalidAgentToken) {
			t.Errorf("ParseAgentToken(%q): expected ErrInvalidAgentToken, got %v", tc, err)
		}
	}
}
