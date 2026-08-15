// Package auth contains MFA (TOTP) functionality.
package auth

import (
	"crypto/rand"
	"fmt"

	"github.com/pquerna/otp/totp"
)

// GenerateTOTPKey generates a new TOTP secret key.
func GenerateTOTPKey() (string, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "LastState",
		AccountName: "user@example.com",
	})
	if err != nil {
		return "", err
	}
	return key.Secret(), nil
}

// ValidateTOTPCode validates a TOTP code against a secret.
func ValidateTOTPCode(secret, code string) bool {
	return totp.Validate(code, secret)
}

// GenerateMfaCode generates a 6-digit MFA code for display.
func GenerateMfaCode() (string, error) {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	code := int(b[0])%9000 + 1000
	return fmt.Sprintf("%04d", code), nil
}
