package totp

import (
	"time"

	"github.com/pquerna/otp"
	ptotp "github.com/pquerna/otp/totp"
)

// Verifier wraps pquerna/otp for TOTP generation and verification.
type Verifier struct{}

// New returns a Verifier.
func New() *Verifier { return &Verifier{} }

// Generate creates a new TOTP secret and returns the secret (base32) and otpauth:// URI.
func (v *Verifier) Generate(issuer, account string) (secret, uri string, err error) {
	key, err := ptotp.Generate(ptotp.GenerateOpts{
		Issuer:      issuer,
		AccountName: account,
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

// Verify returns true if code is valid for secret at time now (unix seconds).
func (v *Verifier) Verify(secret, code string, now int64) bool {
	ok, _ := ptotp.ValidateCustom(code, secret, time.Unix(now, 0), ptotp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	return ok
}
