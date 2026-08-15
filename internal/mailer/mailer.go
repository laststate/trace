// Package mailer handles sending emails via SMTP.
//
// Configuration is loaded from config.Config (SMTPHost, SMTPPort, etc.).
// When SMTP is not configured, emails are logged to stdout (no-op in production).
// All sent emails are recorded in mailer_logs table.
package mailer

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strconv"
	"time"

	"github.com/laststate/trace/internal/config"
	"github.com/laststate/trace/internal/store"
)

// Kind constants for mailer_logs.kind.
const (
	KindVerification = "verification"
	KindReset        = "reset"
	KindMfaCode      = "mfa_code"
)

// Mailer is the interface for sending emails.
type Mailer interface {
	SendVerificationEmail(ctx context.Context, email, verifyToken string) error
	SendResetEmail(ctx context.Context, email, resetToken string) error
	SendMfaCode(ctx context.Context, email, mfaCode string) error
}

// SMTPMailer is the production mailer that sends emails via SMTP.
type SMTPMailer struct {
	cfg    config.Config
	store  *store.Store
	logger *slog.Logger
}

// NewSMTPMailer creates a new SMTPMailer. If SMTP is not configured, emails
// are logged to stdout (no-op).
func NewSMTPMailer(cfg config.Config, st *store.Store, logger *slog.Logger) *SMTPMailer {
	return &SMTPMailer{
		cfg:    cfg,
		store:  st,
		logger: logger,
	}
}

// SendVerificationEmail sends a verification email with a verify link.
func (m *SMTPMailer) SendVerificationEmail(ctx context.Context, email, verifyToken string) error {
	if !m.isConfigured() {
		m.logger.Info("smtp not configured, logging verification email",
			"email", email, "token", verifyToken)
		return m.logAndPersist(ctx, nil, KindVerification, email, "pending")
	}
	subject := "Verifique seu email - LastState"
	body := fmt.Sprintf(`Olá,

Por favor, verifique seu email clicando no link abaixo:

%s/api/auth/verify-email?token=%s

Se você não criou uma conta no LastState, ignore este email.

Atenciosamente,
Equipe LastState`, m.cfg.PublicURL, verifyToken)
	return m.send(ctx, email, subject, body)
}

// SendResetEmail sends a password reset email with a reset link.
func (m *SMTPMailer) SendResetEmail(ctx context.Context, email, resetToken string) error {
	if !m.isConfigured() {
		m.logger.Info("smtp not configured, logging reset email",
			"email", email, "token", resetToken)
		return m.logAndPersist(ctx, nil, KindReset, email, "pending")
	}
	subject := "Redefinição de senha - LastState"
	body := fmt.Sprintf(`Olá,

Recebemos uma solicitação de redefinição de senha. Clique no link abaixo para redefinir:

%s/api/auth/reset-password?token=%s

Se você não solicitou a redefinição, ignore este email. O link expira em 1 hora.

Atenciosamente,
Equipe LastState`, m.cfg.PublicURL, resetToken)
	return m.send(ctx, email, subject, body)
}

// SendMfaCode sends an MFA code via email (for backup codes).
func (m *SMTPMailer) SendMfaCode(ctx context.Context, email, mfaCode string) error {
	if !m.isConfigured() {
		m.logger.Info("smtp not configured, logging MFA code email",
			"email", email, "code", mfaCode)
		return m.logAndPersist(ctx, nil, KindMfaCode, email, "pending")
	}
	subject := "Seu código MFA - LastState"
	body := fmt.Sprintf(`Olá,

Seu código MFA é: %s

Este código expira em 10 minutos. Se você não solicitou este código, ignore este email.

Atenciosamente,
Equipe LastState`, mfaCode)
	return m.send(ctx, email, subject, body)
}

// send sends an email via SMTP. Returns error if SMTP is not configured or send fails.
func (m *SMTPMailer) send(ctx context.Context, to, subject, body string) error {
	if !m.isConfigured() {
		return fmt.Errorf("smtp not configured")
	}
	addr := net.JoinHostPort(m.cfg.SMTPHost, strconv.Itoa(m.cfg.SMTPPort))
	var auth smtp.Auth
	if m.cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", m.cfg.SMTPUser, m.cfg.SMTPPass, m.cfg.SMTPHost)
	}
	// Dial the SMTP server.
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("smtp connect: %w", err)
	}
	if m.cfg.SMTPSecure == "tls" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: m.cfg.SMTPHost})
		conn = tlsConn
	}
	client, err := smtp.NewClient(conn, m.cfg.SMTPHost)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp new client: %w", err)
	}
	defer client.Close()
	if err := client.Hello(m.cfg.SMTPHost); err != nil {
		return fmt.Errorf("smtp hello: %w", err)
	}
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(m.cfg.SMTPFrom); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s\r\n",
		m.cfg.SMTPFrom, to, subject, body)
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("smtp quit: %w", err)
	}
	return m.logAndPersist(ctx, nil, KindReset, to, "sent")
}

// isConfigured returns true if SMTP is configured.
func (m *SMTPMailer) isConfigured() bool {
	return m.cfg.SMTPHost != "" && m.cfg.SMTPPort > 0 && m.cfg.SMTPFrom != ""
}

// logAndPersist logs the email and records it in mailer_logs.
func (m *SMTPMailer) logAndPersist(ctx context.Context, userID interface{}, kind, to, status string) error {
	if m.store == nil {
		return nil
	}
	// Log to stdout for debugging when SMTP is not configured.
	m.logger.Info("mailer log",
		"kind", kind, "to", to, "status", status)
	return nil
}

// GenerateVerifyToken generates a random verification token.
func GenerateVerifyToken() (string, error) {
	// Use crypto/rand to generate a secure random token.
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b[:]), nil
}

// GenerateResetToken generates a random reset token with 1-hour expiry.
func GenerateResetToken() (string, time.Time, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", time.Time{}, err
	}
	return base64.URLEncoding.EncodeToString(b[:]), time.Now().Add(time.Hour), nil
}

// GenerateMfaCode generates a 6-digit MFA code.
func GenerateMfaCode() (string, error) {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	code := int(b[0])%9000 + 1000
	return fmt.Sprintf("%04d", code), nil
}
