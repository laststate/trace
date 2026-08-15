package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/auth"
	"github.com/laststate/trace/internal/oidc"
	"github.com/laststate/trace/internal/webhook"
)

func (s *Server) apiAlerts(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	items, err := s.Store.ListAlertRules(r.Context(), p.ID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiCreateAlert(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	var body struct {
		Name        string `json:"name"`
		Kind        string `json:"kind"`
		Channel     string `json:"channel"`
		TargetURL   string `json:"target_url"`
		Secret      string `json:"secret"`
		CooldownSec int    `json:"cooldown_sec"`
		MaxRetries  int    `json:"max_retries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	if body.Name == "" {
		body.Name = body.Kind
	}
	if body.Channel == "" {
		body.Channel = "webhook"
	}
	switch body.Kind {
	case "new_issue", "new_fatal_issue", "issue_resolved":
	default:
		writeErr(w, 400, "bad_kind", "kind must be new_issue|new_fatal_issue|issue_resolved", false)
		return
	}
	if body.Channel == "webhook" && body.TargetURL != "" {
		if err := webhook.ValidateURL(body.TargetURL); err != nil {
			writeErr(w, 400, "ssrf_blocked", err.Error(), false)
			return
		}
	}
	rule, err := s.Store.CreateAlertRule(r.Context(), p.ID, body.Name, body.Kind, body.Channel, body.TargetURL, body.Secret, body.CooldownSec, body.MaxRetries)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	var actor *uuid.UUID
	if sess, ok := sessionFrom(s, r); ok {
		actor = &sess.UserID
	}
	oid, pid := p.OrganizationID, p.ID
	s.Store.Audit(r.Context(), actor, nil, &oid, &pid, "alert.create", "alert_rule", rule.ID.String(), clientIP(r), r.UserAgent(), map[string]any{"kind": body.Kind})
	writeJSON(w, 201, rule)
}

func (s *Server) apiUpdateAlert(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	var body struct {
		Name        string `json:"name"`
		Enabled     *bool  `json:"enabled"`
		TargetURL   string `json:"target_url"`
		Secret      string `json:"secret"`
		CooldownSec int    `json:"cooldown_sec"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	if body.TargetURL != "" {
		if err := webhook.ValidateURL(body.TargetURL); err != nil {
			writeErr(w, 400, "ssrf_blocked", err.Error(), false)
			return
		}
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	rule, err := s.Store.UpdateAlertRule(r.Context(), p.ID, id, body.Name, enabled, body.TargetURL, body.Secret, body.CooldownSec)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	writeJSON(w, 200, rule)
}

func (s *Server) apiWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	items, err := s.Store.ListWebhookDeliveries(r.Context(), p.ID, 100)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) oidcLogin(w http.ResponseWriter, r *http.Request) {
	cfg := oidc.Config{
		Issuer: s.Cfg.OIDCIssuer, ClientID: s.Cfg.OIDCClientID,
		ClientSecret: s.Cfg.OIDCClientSecret, RedirectURL: s.Cfg.OIDCRedirectURL,
	}
	if !cfg.Enabled() {
		writeErr(w, 404, "oidc_disabled", "OIDC not configured", false)
		return
	}
	p, err := oidc.Discover(r.Context(), cfg.Issuer)
	if err != nil {
		writeErr(w, 502, "oidc_discovery", err.Error(), true)
		return
	}
	state := auth.RandomHex(16)
	nonce := auth.RandomHex(16)
	verifier := auth.RandomHex(32)
	challenge := oidc.S256Challenge(verifier)
	http.SetCookie(w, &http.Cookie{Name: "oidc_state", Value: state, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600, Secure: strings.HasPrefix(s.Cfg.PublicURL, "https")})
	http.SetCookie(w, &http.Cookie{Name: "oidc_nonce", Value: nonce, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600, Secure: strings.HasPrefix(s.Cfg.PublicURL, "https")})
	http.SetCookie(w, &http.Cookie{Name: "oidc_pkce", Value: verifier, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600, Secure: strings.HasPrefix(s.Cfg.PublicURL, "https")})
	http.Redirect(w, r, cfg.AuthCodeURL(p, state, nonce, challenge), http.StatusFound)
}

func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	cfg := oidc.Config{
		Issuer: s.Cfg.OIDCIssuer, ClientID: s.Cfg.OIDCClientID,
		ClientSecret: s.Cfg.OIDCClientSecret, RedirectURL: s.Cfg.OIDCRedirectURL,
	}
	if !cfg.Enabled() {
		writeErr(w, 404, "oidc_disabled", "OIDC not configured", false)
		return
	}
	c, err := r.Cookie("oidc_state")
	if err != nil || c.Value == "" || c.Value != r.URL.Query().Get("state") {
		writeErr(w, 400, "bad_state", "invalid oauth state", false)
		return
	}
	nonceC, _ := r.Cookie("oidc_nonce")
	pkceC, _ := r.Cookie("oidc_pkce")
	nonce, verifier := "", ""
	if nonceC != nil {
		nonce = nonceC.Value
	}
	if pkceC != nil {
		verifier = pkceC.Value
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		writeErr(w, 400, "missing_code", "no code", false)
		return
	}
	p, err := oidc.Discover(r.Context(), cfg.Issuer)
	if err != nil {
		writeErr(w, 502, "oidc_discovery", err.Error(), true)
		return
	}
	tok, err := cfg.Exchange(r.Context(), p, code, verifier)
	if err != nil {
		writeErr(w, 502, "oidc_token", err.Error(), true)
		return
	}
	claims, err := cfg.ValidateIDToken(r.Context(), p, tok.IDToken, nonce)
	if err != nil {
		writeErr(w, 401, "oidc_id_token", err.Error(), false)
		return
	}
	if err := oidc.RequireVerified(claims.EmailVerified); err != nil {
		writeErr(w, 403, "email_unverified", err.Error(), false)
		return
	}
	email := claims.Email
	name := claims.Name
	if email == "" {
		// fall back to userinfo only if ID token lacked email
		ui, uerr := cfg.UserInfo(r.Context(), p, tok.AccessToken)
		if uerr != nil {
			writeErr(w, 502, "oidc_userinfo", uerr.Error(), true)
			return
		}
		if err := oidc.RequireVerified(ui.EmailVerified); err != nil {
			writeErr(w, 403, "email_unverified", err.Error(), false)
			return
		}
		email, name = ui.Email, ui.Name
	}
	_, sess, secret, err := s.Store.UpsertOIDCUserOpts(r.Context(), email, name, s.Cfg.OIDCAutoJoin)
	if err != nil {
		writeErr(w, 403, "no_membership", err.Error(), false)
		return
	}
	uid := sess.UserID
	oid := sess.OrganizationID
	s.Store.Audit(r.Context(), &uid, nil, &oid, nil, "auth.oidc_login", "user", uid.String(), clientIP(r), r.UserAgent(), map[string]any{"email": email})
	http.SetCookie(w, &http.Cookie{Name: "oidc_state", MaxAge: -1, Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "oidc_nonce", MaxAge: -1, Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "oidc_pkce", MaxAge: -1, Path: "/"})
	s.setSessionCookie(w, secret)
	// Prefer cookie session; hash token kept for legacy SPA bootstrap once
	loc := "/overview"
	http.Redirect(w, r, loc, http.StatusFound)
}

func (s *Server) apiListOrgs(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		// open UI: list none
		writeJSON(w, 200, map[string]any{"items": []any{}})
		return
	}
	items, err := s.Store.ListOrganizationsForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiCreateOrg(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	var body struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	if body.Name == "" {
		writeErr(w, 400, "bad_request", "name required", false)
		return
	}
	if body.Slug == "" {
		body.Slug = body.Name
	}
	o, err := s.Store.CreateOrganization(r.Context(), body.Name, body.Slug, sess.UserID)
	if err != nil {
		writeErr(w, 400, "create_failed", err.Error(), false)
		return
	}
	uid := sess.UserID
	s.Store.Audit(r.Context(), &uid, nil, &o.ID, nil, "org.create", "organization", o.ID.String(), clientIP(r), r.UserAgent(), map[string]any{"slug": o.Slug})
	writeJSON(w, 201, o)
}

func (s *Server) apiListMembers(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	items, err := s.Store.ListMembers(r.Context(), sess.OrganizationID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiInviteMember(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	var body struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	if body.Email == "" {
		writeErr(w, 400, "bad_request", "email required", false)
		return
	}
	by := sess.UserID
	secret, err := s.Store.InviteMember(r.Context(), sess.OrganizationID, body.Email, body.Role, &by)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	s.Store.Audit(r.Context(), &by, nil, &sess.OrganizationID, nil, "org.invite", "invite", body.Email, clientIP(r), r.UserAgent(), map[string]any{"role": body.Role})
	// return invite token once (email delivery is integration)
	writeJSON(w, 201, map[string]any{"email": body.Email, "invite_token": secret})
}

func (s *Server) apiListProjects(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		p, err := s.Store.DefaultProject(r.Context())
		if err != nil {
			writeJSON(w, 200, map[string]any{"items": []any{}})
			return
		}
		writeJSON(w, 200, map[string]any{"items": []any{p}})
		return
	}
	items, err := s.Store.ListProjects(r.Context(), sess.OrganizationID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiCreateProject(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	var body struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	if body.Name == "" {
		writeErr(w, 400, "bad_request", "name required", false)
		return
	}
	if body.Slug == "" {
		body.Slug = body.Name
	}
	p, err := s.Store.CreateProject(r.Context(), sess.OrganizationID, body.Name, body.Slug, body.Description)
	if err != nil {
		writeErr(w, 400, "create_failed", err.Error(), false)
		return
	}
	uid := sess.UserID
	oid := sess.OrganizationID
	s.Store.Audit(r.Context(), &uid, nil, &oid, &p.ID, "project.create", "project", p.ID.String(), clientIP(r), r.UserAgent(), nil)
	writeJSON(w, 201, p)
}

func (s *Server) apiUpdateProject(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	p, err := s.Store.UpdateProject(r.Context(), sess.OrganizationID, id, body.Name, body.Description)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) apiDeleteProject(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	if err := s.Store.DeleteProject(r.Context(), sess.OrganizationID, id); err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	uid := sess.UserID
	oid := sess.OrganizationID
	s.Store.Audit(r.Context(), &uid, nil, &oid, &id, "project.delete", "project", id.String(), clientIP(r), r.UserAgent(), nil)
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}

func (s *Server) apiListRelays(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	items, err := s.Store.ListRelays(r.Context(), p.ID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiListHardware(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	items, err := s.Store.ListHardwareRevisions(r.Context(), p.ID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiCreateHardware(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	var body struct {
		Revision string `json:"revision"`
		BOM      string `json:"bom"`
		Lot      string `json:"lot"`
		Notes    string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	if body.Revision == "" {
		writeErr(w, 400, "bad_request", "revision required", false)
		return
	}
	id, err := s.Store.CreateHardwareRevision(r.Context(), p.ID, body.Revision, body.BOM, body.Lot, body.Notes)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 201, map[string]any{"id": id})
}

func (s *Server) openapi(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(openapiJSON))
}
