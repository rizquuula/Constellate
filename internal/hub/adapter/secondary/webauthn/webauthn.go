package webauthn

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// Provider wraps *webauthn.WebAuthn and exposes the methods the auth use case
// needs via the auth.WebAuthn port. DTOs cross the boundary as JSON bytes so
// the use case (and domain layer) stays vendor-light.
type Provider struct {
	wa *webauthn.WebAuthn
}

// New constructs a Provider. rpID must be the effective domain (no scheme, no
// port). origins should be the full origins the browser will be running from.
func New(rpID string, origins []string) (*Provider, error) {
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: "Constellate",
		RPOrigins:     origins,
	})
	if err != nil {
		return nil, fmt.Errorf("webauthn: init: %w", err)
	}
	return &Provider{wa: wa}, nil
}

// operatorUser implements webauthn.User for the single operator account.
// The user handle is fixed ("operator" as UTF-8 bytes) because Constellate has
// exactly one operator.
type operatorUser struct {
	creds []webauthn.Credential
}

var operatorHandle = []byte("operator")

func (u operatorUser) WebAuthnID() []byte                { return operatorHandle }
func (u operatorUser) WebAuthnName() string              { return "operator" }
func (u operatorUser) WebAuthnDisplayName() string       { return "operator" }
func (u operatorUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

// unmarshalCreds deserializes a slice of JSON-encoded webauthn.Credential blobs.
func unmarshalCreds(raw [][]byte) ([]webauthn.Credential, error) {
	creds := make([]webauthn.Credential, 0, len(raw))
	for _, b := range raw {
		var c webauthn.Credential
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("webauthn: unmarshal credential: %w", err)
		}
		creds = append(creds, c)
	}
	return creds, nil
}

// BeginRegistration starts a credential-creation ceremony.
// creds is the set of already-registered credentials (JSON-encoded) — used to
// populate the excludeCredentials list so the same key cannot be re-registered.
// Returns the CredentialCreation options JSON and the SessionData JSON.
func (p *Provider) BeginRegistration(creds [][]byte) (optionsJSON []byte, sessionJSON []byte, err error) {
	parsed, err := unmarshalCreds(creds)
	if err != nil {
		return nil, nil, err
	}
	user := operatorUser{creds: parsed}

	creation, session, err := p.wa.BeginRegistration(user)
	if err != nil {
		return nil, nil, fmt.Errorf("webauthn: begin registration: %w", err)
	}
	optionsJSON, err = json.Marshal(creation)
	if err != nil {
		return nil, nil, fmt.Errorf("webauthn: marshal creation options: %w", err)
	}
	sessionJSON, err = json.Marshal(session)
	if err != nil {
		return nil, nil, fmt.Errorf("webauthn: marshal session: %w", err)
	}
	return optionsJSON, sessionJSON, nil
}

// FinishRegistration completes the credential-creation ceremony.
// body is the raw JSON body from the browser (ParseCredentialCreationResponseBody).
// Returns the new webauthn.Credential serialized as JSON.
func (p *Provider) FinishRegistration(creds [][]byte, sessionJSON []byte, body io.Reader) (newCredJSON []byte, err error) {
	parsed, err := unmarshalCreds(creds)
	if err != nil {
		return nil, err
	}
	user := operatorUser{creds: parsed}

	var session webauthn.SessionData
	if err := json.Unmarshal(sessionJSON, &session); err != nil {
		return nil, fmt.Errorf("webauthn: unmarshal session: %w", err)
	}

	parsedResponse, err := protocol.ParseCredentialCreationResponseBody(body)
	if err != nil {
		return nil, fmt.Errorf("webauthn: parse creation response: %w", err)
	}
	cred, err := p.wa.CreateCredential(user, session, parsedResponse)
	if err != nil {
		return nil, fmt.Errorf("webauthn: create credential: %w", err)
	}
	newCredJSON, err = json.Marshal(cred)
	if err != nil {
		return nil, fmt.Errorf("webauthn: marshal new credential: %w", err)
	}
	return newCredJSON, nil
}

// BeginLogin starts a passkey login ceremony (discoverable / usernameless).
// Returns the CredentialAssertion options JSON and SessionData JSON.
func (p *Provider) BeginLogin(creds [][]byte) (optionsJSON []byte, sessionJSON []byte, err error) {
	parsed, err := unmarshalCreds(creds)
	if err != nil {
		return nil, nil, err
	}
	user := operatorUser{creds: parsed}

	assertion, session, err := p.wa.BeginLogin(user)
	if err != nil {
		return nil, nil, fmt.Errorf("webauthn: begin login: %w", err)
	}
	optionsJSON, err = json.Marshal(assertion)
	if err != nil {
		return nil, nil, fmt.Errorf("webauthn: marshal assertion options: %w", err)
	}
	sessionJSON, err = json.Marshal(session)
	if err != nil {
		return nil, nil, fmt.Errorf("webauthn: marshal login session: %w", err)
	}
	return optionsJSON, sessionJSON, nil
}

// FinishLogin completes the passkey login ceremony.
// body is the raw JSON body from the browser (ParseCredentialRequestResponseBody).
func (p *Provider) FinishLogin(creds [][]byte, sessionJSON []byte, body io.Reader) error {
	parsed, err := unmarshalCreds(creds)
	if err != nil {
		return err
	}
	user := operatorUser{creds: parsed}

	var session webauthn.SessionData
	if err := json.Unmarshal(sessionJSON, &session); err != nil {
		return fmt.Errorf("webauthn: unmarshal login session: %w", err)
	}

	parsedResponse, err := protocol.ParseCredentialRequestResponseBody(body)
	if err != nil {
		return fmt.Errorf("webauthn: parse assertion response: %w", err)
	}
	_, err = p.wa.ValidateLogin(user, session, parsedResponse)
	if err != nil {
		return fmt.Errorf("webauthn: validate login: %w", err)
	}
	return nil
}
