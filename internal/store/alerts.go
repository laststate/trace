package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AlertRule struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"project_id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Enabled   bool      `json:"enabled"`
	Channel   string    `json:"channel"`
	TargetURL string    `json:"target_url"`
	// Secret never returned in list JSON full — prefix only if needed
	HasSecret bool      `json:"has_secret"`
	CreatedAt time.Time `json:"created_at"`
	Secret    string    `json:"-"`
}

type WebhookDelivery struct {
	ID         uuid.UUID       `json:"id"`
	EventType  string          `json:"event_type"`
	TargetURL  string          `json:"target_url"`
	StatusCode int             `json:"status_code"`
	Success    bool            `json:"success"`
	CreatedAt  time.Time       `json:"created_at"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

func (s *Store) CreateAlertRule(ctx context.Context, projectID uuid.UUID, name, kind, channel, target, secret string) (AlertRule, error) {
	var r AlertRule
	err := s.Pool.QueryRow(ctx, `
INSERT INTO alert_rules(project_id,name,kind,channel,target_url,secret,enabled)
VALUES($1,$2,$3,$4,$5,$6,true)
RETURNING id,project_id,name,kind,enabled,channel,target_url,created_at,secret`,
		projectID, name, kind, channel, target, secret).
		Scan(&r.ID, &r.ProjectID, &r.Name, &r.Kind, &r.Enabled, &r.Channel, &r.TargetURL, &r.CreatedAt, &r.Secret)
	r.HasSecret = r.Secret != ""
	return r, err
}

func (s *Store) ListAlertRules(ctx context.Context, projectID uuid.UUID) ([]AlertRule, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id,project_id,name,kind,enabled,channel,target_url,created_at,secret
FROM alert_rules WHERE project_id=$1 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertRule
	for rows.Next() {
		var r AlertRule
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Name, &r.Kind, &r.Enabled, &r.Channel, &r.TargetURL, &r.CreatedAt, &r.Secret); err != nil {
			return nil, err
		}
		r.HasSecret = r.Secret != ""
		r.Secret = "" // don't leak
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) RulesForKind(ctx context.Context, projectID uuid.UUID, kind string) ([]AlertRule, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id,project_id,name,kind,enabled,channel,target_url,created_at,secret
FROM alert_rules WHERE project_id=$1 AND enabled AND kind=$2`, projectID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertRule
	for rows.Next() {
		var r AlertRule
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Name, &r.Kind, &r.Enabled, &r.Channel, &r.TargetURL, &r.CreatedAt, &r.Secret); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) RecordWebhook(ctx context.Context, projectID uuid.UUID, ruleID *uuid.UUID, eventType, target string, status int, ok bool, body string, payload any) error {
	raw, _ := json.Marshal(payload)
	_, err := s.Pool.Exec(ctx, `
INSERT INTO webhook_deliveries(project_id,rule_id,event_type,target_url,status_code,success,response_body,payload)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, projectID, ruleID, eventType, target, status, ok, body, raw)
	return err
}

func (s *Store) ListWebhookDeliveries(ctx context.Context, projectID uuid.UUID, limit int) ([]WebhookDelivery, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,event_type,target_url,status_code,success,created_at,payload
FROM webhook_deliveries WHERE project_id=$1 ORDER BY created_at DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebhookDelivery
	for rows.Next() {
		var d WebhookDelivery
		if err := rows.Scan(&d.ID, &d.EventType, &d.TargetURL, &d.StatusCode, &d.Success, &d.CreatedAt, &d.Payload); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) EnqueueJob(ctx context.Context, typ string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO jobs(type,payload,status) VALUES($1,$2,'ready')`, typ, raw)
	return err
}

// UpsertOIDCUser creates or finds user by email and ensures membership.
func (s *Store) UpsertOIDCUser(ctx context.Context, email, name string) (User, Session, string, error) {
	org, err := s.firstOrg(ctx)
	if err != nil {
		return User{}, Session{}, "", err
	}
	var u User
	err = s.Pool.QueryRow(ctx, `SELECT id,email,name FROM users WHERE email=$1`, email).Scan(&u.ID, &u.Email, &u.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		ph, _ := HashPassword("oidc-disabled-" + uuid.NewString())
		err = s.Pool.QueryRow(ctx, `INSERT INTO users(email,name,password_hash) VALUES($1,$2,$3) RETURNING id,email,name`,
			email, name, ph).Scan(&u.ID, &u.Email, &u.Name)
		if err != nil {
			return User{}, Session{}, "", err
		}
		_, _ = s.Pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'developer') ON CONFLICT (organization_id, user_id) DO NOTHING`, org.ID, u.ID)
	} else if err != nil {
		return User{}, Session{}, "", err
	}
	var role string
	_ = s.Pool.QueryRow(ctx, `SELECT role FROM memberships WHERE user_id=$1 LIMIT 1`, u.ID).Scan(&role)
	if role == "" {
		role = "developer"
		_, _ = s.Pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,$3) ON CONFLICT (organization_id, user_id) DO NOTHING`, org.ID, u.ID, role)
	}
	sess, secret, err := s.MintSession(ctx, u, org.ID, role)
	return u, sess, secret, err
}
