package api

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/laststate/trace/internal/auth"
	"github.com/laststate/trace/internal/oidc"
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
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		Channel   string `json:"channel"`
		TargetURL string `json:"target_url"`
		Secret    string `json:"secret"`
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
	rule, err := s.Store.CreateAlertRule(r.Context(), p.ID, body.Name, body.Kind, body.Channel, body.TargetURL, body.Secret)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	pid := p.ID
	s.Store.Audit(r.Context(), nil, nil, &p.OrganizationID, &pid, "alert.create", "alert_rule", rule.ID.String(), clientIP(r), r.UserAgent(), map[string]any{"kind": body.Kind})
	writeJSON(w, 201, rule)
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
	http.SetCookie(w, &http.Cookie{Name: "oidc_state", Value: state, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	http.Redirect(w, r, cfg.AuthCodeURL(p, state), http.StatusFound)
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
	tok, err := cfg.Exchange(r.Context(), p, code)
	if err != nil {
		writeErr(w, 502, "oidc_token", err.Error(), true)
		return
	}
	ui, err := cfg.UserInfo(r.Context(), p, tok.AccessToken)
	if err != nil {
		writeErr(w, 502, "oidc_userinfo", err.Error(), true)
		return
	}
	_, sess, secret, err := s.Store.UpsertOIDCUser(r.Context(), ui.Email, ui.Name)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	uid := sess.UserID
	oid := sess.OrganizationID
	s.Store.Audit(r.Context(), &uid, nil, &oid, nil, "auth.oidc_login", "user", uid.String(), clientIP(r), r.UserAgent(), map[string]any{"email": ui.Email})
	// redirect to UI with token in hash (local SPA picks it up)
	loc := "/#token=" + url.QueryEscape(secret)
	http.Redirect(w, r, loc, http.StatusFound)
}
