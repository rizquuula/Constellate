package totp

import (
	"crypto/subtle"
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

// Matches checks whether code is valid for secret at the current 30-second step
// containing now (unix seconds). It checks the current step and one step in each
// direction (Skew=1). On success it returns the matched step number and ok=true.
// Comparison is constant-time to avoid timing oracles.
func (v *Verifier) Matches(secret, code string, now int64) (step int64, ok bool) {
	currentStep := now / 30
	opts := ptotp.ValidateOpts{
		Period:    30,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	}
	for _, s := range []int64{currentStep - 1, currentStep, currentStep + 1} {
		expected, err := ptotp.GenerateCodeCustom(secret, time.Unix(s*30, 0), opts)
		if err != nil {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return s, true
		}
	}
	return 0, false
}

// Verify returns true if code is valid for secret at time now (unix seconds).
func (v *Verifier) Verify(secret, code string, now int64) bool {
	_, ok := v.Matches(secret, code, now)
	return ok
}
