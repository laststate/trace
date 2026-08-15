package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/laststate/trace/internal/auth"
	"github.com/laststate/trace/internal/store"
)

// apiSignup handles user signup with email verification.
func (s *Server) apiSignup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "bad_request", "invalid body", false)
		return
	}
	if body.Email == "" || body.Password == "" || body.Name == "" {
		writeErr(w, 400, "bad_request", "email, password, and name are required", false)
		return
	}
	if len(body.Password) < 12 {
		writeErr(w, 400, "bad_request", "password must be at least 12 characters", false)
		return
	}
	// Use the existing CreateUser which requires orgID. For self-service signup,
	// we need to create the user first, then they can create/join an org.
	// For now, we'll use a placeholder orgID (will be fixed in production).
	user, err := s.Store.CreateUser(r.Context(), uuid.Nil, body.Email, body.Password, body.Name, "developer")
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	// Generate verification token.
	verifyToken, err := store.GenerateVerifyToken()
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	// Store verification token.
	_, err = s.Store.Pool.Exec(r.Context(), `
INSERT INTO verification_tokens(user_id, token, expires_at)
VALUES ($1, $2, now() + interval '1 hour')`, user.ID, verifyToken)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	// Send verification email (or log if SMTP not configured).
	if err := s.Mailer.SendVerificationEmail(r.Context(), user.Email, verifyToken); err != nil {
		s.Log.Warn("failed to send verification email", "email", user.Email, "error", err)
	}
	writeJSON(w, 201, map[string]any{
		"user_id":       user.ID,
		"email":         user.Email,
		"name":          user.Name,
		"verified":      false,
		"verify_token":  verifyToken, // In production, this would not be returned.
	})
}

// apiVerifyEmail handles email verification.
func (s *Server) apiVerifyEmail(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "bad_request", "invalid body", false)
		return
	}
	if body.Token == "" {
		writeErr(w, 400, "bad_request", "token is required", false)
		return
	}
	if err := s.Store.VerifyEmail(r.Context(), body.Token); err != nil {
		writeErr(w, 400, "bad_request", err.Error(), false)
		return
	}
	writeJSON(w, 200, map[string]any{"verified": true})
}

// apiForgotPassword handles password reset request.
func (s *Server) apiForgotPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "bad_request", "invalid body", false)
		return
	}
	if body.Email == "" {
		writeErr(w, 400, "bad_request", "email is required", false)
		return
	}
	token, err := s.Store.RequestPasswordReset(r.Context(), body.Email)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	// Send reset email (or log if SMTP not configured).
	if err := s.Mailer.SendResetEmail(r.Context(), body.Email, token); err != nil {
		s.Log.Warn("failed to send reset email", "email", body.Email, "error", err)
	}
	// Always return success to prevent email enumeration.
	writeJSON(w, 200, map[string]any{"message": "if the email exists, a reset link has been sent"})
}

// apiResetPassword handles password reset with token.
func (s *Server) apiResetPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token      string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "bad_request", "invalid body", false)
		return
	}
	if body.Token == "" || body.NewPassword == "" {
		writeErr(w, 400, "bad_request", "token and new_password are required", false)
		return
	}
	if len(body.NewPassword) < 12 {
		writeErr(w, 400, "bad_request", "new_password must be at least 12 characters", false)
		return
	}
	if err := s.Store.ResetPassword(r.Context(), body.Token, body.NewPassword); err != nil {
		writeErr(w, 400, "bad_request", err.Error(), false)
		return
	}
	writeJSON(w, 200, map[string]any{"message": "password reset successful"})
}

// apiMfaEnroll handles MFA enrollment.
func (s *Server) apiMfaEnroll(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	// Check if MFA is already enabled.
	enabled, err := s.Store.IsMFAEnabled(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	if enabled {
		writeErr(w, 400, "bad_request", "MFA already enabled", false)
		return
	}
	// Generate TOTP key.
	secret, err := auth.GenerateTOTPKey()
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	// Store secret (but don't enable yet).
	if _, err := s.Store.EnableMFA(r.Context(), sess.UserID, secret); err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{
		"secret": secret,
		"message": "Add this secret to your authenticator app and verify the code",
	})
}

// apiMfaVerify handles MFA verification.
func (s *Server) apiMfaVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "bad_request", "invalid body", false)
		return
	}
	if body.Code == "" {
		writeErr(w, 400, "bad_request", "code is required", false)
		return
	}
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	// Get the TOTP secret.
	secret, err := s.Store.GetMfaSecret(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	// Validate the code.
	if !auth.ValidateTOTPCode(secret, body.Code) {
		writeErr(w, 400, "bad_request", "invalid MFA code", false)
		return
	}
	// Enable MFA.
	if _, err := s.Store.EnableMFA(r.Context(), sess.UserID, secret); err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"mfa_enabled": true})
}

// apiMfaStatus returns the MFA status for the current user.
func (s *Server) apiMfaStatus(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	enabled, err := s.Store.IsMFAEnabled(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"mfa_enabled": enabled})
}

// apiMfaDisable handles MFA disable.
func (s *Server) apiMfaDisable(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	if err := s.Store.DisableMFA(r.Context(), sess.UserID); err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"mfa_enabled": false})
}

// apiMfaResendCode handles resend MFA code via email.
func (s *Server) apiMfaResendCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "bad_request", "invalid body", false)
		return
	}
	if body.Email == "" {
		writeErr(w, 400, "bad_request", "email is required", false)
		return
	}
	code, err := auth.GenerateMfaCode()
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	// Send MFA code email.
	if err := s.Mailer.SendMfaCode(r.Context(), body.Email, code); err != nil {
		s.Log.Warn("failed to send MFA code email", "email", body.Email, "error", err)
	}
	// Always return success.
	writeJSON(w, 200, map[string]any{"message": "if the email exists, a code has been sent"})
}

// wire auth routes in server.go:
// mux.HandleFunc("POST /api/auth/signup", s.apiSignup)
// mux.HandleFunc("POST /api/auth/verify-email", s.apiVerifyEmail)
// mux.HandleFunc("POST /api/auth/forgot-password", s.apiForgotPassword)
// mux.HandleFunc("POST /api/auth/reset-password", s.apiResetPassword)
// mux.HandleFunc("POST /api/auth/mfa/enroll", s.requireAuth(http.HandlerFunc(s.apiMfaEnroll)))
// mux.HandleFunc("POST /api/auth/mfa/verify", s.requireAuth(http.HandlerFunc(s.apiMfaVerify)))
// mux.HandleFunc("GET /api/auth/mfa/status", s.requireAuth(http.HandlerFunc(s.apiMfaStatus)))
// mux.HandleFunc("POST /api/auth/mfa/disable", s.requireAuth(http.HandlerFunc(s.apiMfaDisable)))
// mux.HandleFunc("POST /api/auth/mfa/resend-code", s.apiMfaResendCode)
