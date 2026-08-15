package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrEmailAlreadyVerified is returned when trying to verify an already verified email.
var ErrEmailAlreadyVerified = errors.New("store: email already verified")

// ErrInvalidResetToken is returned when the reset token is invalid or expired.
var ErrInvalidResetToken = errors.New("store: invalid or expired reset token")

// ErrMFARequired is returned when MFA is required but not provided.
var ErrMFARequired = errors.New("store: MFA required")

// VerifyEmail verifies a user's email using a token.
func (s *Store) VerifyEmail(ctx context.Context, token string) error {
	var userID uuid.UUID
	var expiresAt time.Time
	err := s.Pool.QueryRow(ctx, `
SELECT user_id, expires_at FROM verification_tokens WHERE token=$1`, token).
		Scan(&userID, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidResetToken
		}
		return err
	}
	if time.Now().After(expiresAt) {
		return ErrInvalidResetToken
	}
	// Mark email as verified.
	_, err = s.Pool.Exec(ctx, `
UPDATE users SET email_verified_at=now(), updated_at=now() WHERE id=$1`, userID)
	if err != nil {
		return err
	}
	// Delete the token.
	_, err = s.Pool.Exec(ctx, `DELETE FROM verification_tokens WHERE user_id=$1`, userID)
	return err
}

// RequestPasswordReset generates a reset token and returns it.
func (s *Store) RequestPasswordReset(ctx context.Context, email string) (string, error) {
	var userID uuid.UUID
	err := s.Pool.QueryRow(ctx, `SELECT id FROM users WHERE email=$1`, email).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Return a dummy token to prevent email enumeration.
			return GenerateResetToken()
		}
		return "", err
	}
	token, err := GenerateResetToken()
	if err != nil {
		return "", err
	}
	_, err = s.Pool.Exec(ctx, `
INSERT INTO password_reset_tokens(user_id, token, expires_at)
VALUES ($1, $2, now() + interval '1 hour')
ON CONFLICT (user_id) DO UPDATE SET token=$2, expires_at=now() + interval '1 hour'`,
		userID, token)
	if err != nil {
		return "", err
	}
	return token, nil
}

// ResetPassword resets a user's password using a reset token.
func (s *Store) ResetPassword(ctx context.Context, token, newPassword string) error {
	var userID uuid.UUID
	var expiresAt time.Time
	err := s.Pool.QueryRow(ctx, `
SELECT user_id, expires_at FROM password_reset_tokens WHERE token=$1`, token).
		Scan(&userID, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidResetToken
		}
		return err
	}
	if time.Now().After(expiresAt) {
		return ErrInvalidResetToken
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `
UPDATE users SET password_hash=$1, updated_at=now() WHERE id=$2`, hash, userID)
	if err != nil {
		return err
	}
	// Delete the token.
	_, err = s.Pool.Exec(ctx, `DELETE FROM password_reset_tokens WHERE user_id=$1`, userID)
	return err
}

// GenerateVerifyToken generates a random verification token.
func GenerateVerifyToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b[:]), nil
}

// GenerateResetToken generates a random reset token.
func GenerateResetToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b[:]), nil
}

// EnableMFA enables MFA for a user.
func (s *Store) EnableMFA(ctx context.Context, userID uuid.UUID, secret string) (bool, error) {
	ct, err := s.Pool.Exec(ctx, `
INSERT INTO mfa_secrets(user_id, secret, is_enabled, created_at)
VALUES ($1, $2, true, now())
ON CONFLICT (user_id) DO UPDATE SET secret=$2, is_enabled=true, updated_at=now()`,
		userID, secret)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

// DisableMFA disables MFA for a user.
func (s *Store) DisableMFA(ctx context.Context, userID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `
UPDATE mfa_secrets SET is_enabled=false, updated_at=now() WHERE user_id=$1`, userID)
	return err
}

// IsMFAEnabled checks if MFA is enabled for a user.
func (s *Store) IsMFAEnabled(ctx context.Context, userID uuid.UUID) (bool, error) {
	var enabled bool
	err := s.Pool.QueryRow(ctx, `
SELECT is_enabled FROM mfa_secrets WHERE user_id=$1`, userID).Scan(&enabled)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return enabled, nil
}

// GetMfaSecret returns the TOTP secret for a user.
func (s *Store) GetMfaSecret(ctx context.Context, userID uuid.UUID) (string, error) {
	var secret string
	err := s.Pool.QueryRow(ctx, `
SELECT secret FROM mfa_secrets WHERE user_id=$1`, userID).Scan(&secret)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errors.New("store: MFA not enabled for user")
		}
		return "", err
	}
	return secret, nil
}
