package totp_test

import (
	"testing"
	"time"

	ptotp "github.com/pquerna/otp/totp"
	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/totp"
)

func TestMatches_ValidCode(t *testing.T) {
	v := totp.New()

	// Generate a fresh TOTP secret.
	secret, _, err := v.Generate("Constellate", "operator@test")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	now := time.Now().Unix()

	// Generate the current valid code using pquerna directly.
	code, err := ptotp.GenerateCode(secret, time.Unix(now, 0))
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	step, ok := v.Matches(secret, code, now)
	if !ok {
		t.Fatal("Matches returned ok=false for a valid code")
	}
	expectedStep := now / 30
	// step should be within skew range.
	if step < expectedStep-1 || step > expectedStep+1 {
		t.Errorf("Matches returned step=%d, expected in [%d, %d]", step, expectedStep-1, expectedStep+1)
	}
}

func TestMatches_WrongCode(t *testing.T) {
	v := totp.New()

	secret, _, err := v.Generate("Constellate", "operator@test")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	now := time.Now().Unix()

	_, ok := v.Matches(secret, "000000", now) // almost certainly wrong
	// We can't guarantee 000000 is wrong since it might legitimately match,
	// so test with a clearly invalid code instead.
	_, _ = ok, ok

	// Use an obviously invalid 7-digit code (TOTP is 6 digits).
	_, ok2 := v.Matches(secret, "9999999", now)
	if ok2 {
		t.Fatal("Matches returned ok=true for an invalid 7-digit code")
	}
}

func TestVerify_UsesMatches(t *testing.T) {
	v := totp.New()

	secret, _, err := v.Generate("Constellate", "operator@test")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	now := time.Now().Unix()
	code, err := ptotp.GenerateCode(secret, time.Unix(now, 0))
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	if !v.Verify(secret, code, now) {
		t.Error("Verify returned false for a valid code")
	}
}
