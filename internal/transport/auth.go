package transport

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
)

// ErrInvalidAgentToken is returned when a token cannot be parsed or verified.
var ErrInvalidAgentToken = errors.New("transport: invalid agent token")

// BuildAgentToken returns "v1.<machineID>.<unixTs>.<base64url-nopad sig>".
// sig = ed25519.Sign(priv, signingInput(machineID, unixTs)).
func BuildAgentToken(machineID string, priv ed25519.PrivateKey, unixTs int64) string {
	input := signingInput(machineID, unixTs)
	sig := ed25519.Sign(priv, input)
	encoded := base64.RawURLEncoding.EncodeToString(sig)
	return "v1." + machineID + "." + strconv.FormatInt(unixTs, 10) + "." + encoded
}

// ParseAgentToken splits the token and returns the fields.
// Returns ErrInvalidAgentToken on malformed input or non-v1 version.
func ParseAgentToken(token string) (machineID string, unixTs int64, sig []byte, err error) {
	parts := strings.SplitN(token, ".", 4)
	if len(parts) != 4 {
		return "", 0, nil, ErrInvalidAgentToken
	}
	if parts[0] != "v1" {
		return "", 0, nil, ErrInvalidAgentToken
	}
	machineID = parts[1]
	if machineID == "" {
		return "", 0, nil, ErrInvalidAgentToken
	}
	unixTs, err = strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", 0, nil, ErrInvalidAgentToken
	}
	sig, err = base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return "", 0, nil, ErrInvalidAgentToken
	}
	return machineID, unixTs, sig, nil
}

// VerifyAgentToken checks the signature with pub and that |now-unixTs| <= skewSeconds.
func VerifyAgentToken(pub ed25519.PublicKey, machineID string, unixTs int64, sig []byte, now, skewSeconds int64) error {
	diff := now - unixTs
	if diff < 0 {
		diff = -diff
	}
	if diff > skewSeconds {
		return ErrInvalidAgentToken
	}
	input := signingInput(machineID, unixTs)
	if !ed25519.Verify(pub, input, sig) {
		return ErrInvalidAgentToken
	}
	return nil
}

// signingInput returns the canonical bytes to sign/verify:
// "constellate-agent-auth.v1." + machineID + "." + strconv.FormatInt(ts,10)
func signingInput(machineID string, ts int64) []byte {
	return []byte("constellate-agent-auth.v1." + machineID + "." + strconv.FormatInt(ts, 10))
}
