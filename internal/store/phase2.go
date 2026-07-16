package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/laststate/trace/internal/auth"
)

type User struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
	Name  string    `json:"name"`
}

type Session struct {
	UserID         uuid.UUID
	Email          string
	Name           string
	OrganizationID uuid.UUID
	Role           string
	SessionID      uuid.UUID
}

type AuditEntry struct {
	ID        uuid.UUID       `json:"id"`
	Action    string          `json:"action"`
	Target    string          `json:"target_type"`
	TargetID  string          `json:"target_id"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedAt time.Time       `json:"created_at"`
	Actor     string          `json:"actor,omitempty"`
}

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// EnsureAdmin creates admin user if none exist (idempotent).
func (s *Store) EnsureAdmin(ctx context.Context, email, password, name string) (User, string, error) {
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return User{}, "", err
	}
	if n > 0 {
		return User{}, "", fmtAlready()
	}
	org, err := s.firstOrg(ctx)
	if err != nil {
		return User{}, "", err
	}
	ph, err := HashPassword(password)
	if err != nil {
		return User{}, "", err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return User{}, "", err
	}
	defer tx.Rollback(ctx)
	var u User
	if err := tx.QueryRow(ctx, `INSERT INTO users(email,name,password_hash) VALUES($1,$2,$3) RETURNING id,email,name`,
		email, name, ph).Scan(&u.ID, &u.Email, &u.Name); err != nil {
		return User{}, "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')`, org.ID, u.ID); err != nil {
		return User{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, "", err
	}
	return u, password, nil
}

func fmtAlready() error { return errors.New("admin already exists") }

func (s *Store) firstOrg(ctx context.Context) (Org, error) {
	var o Org
	err := s.Pool.QueryRow(ctx, `SELECT id,name,slug FROM organizations ORDER BY created_at ASC LIMIT 1`).
		Scan(&o.ID, &o.Name, &o.Slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return Org{}, ErrNotFound
	}
	return o, err
}

func (s *Store) Login(ctx context.Context, email, password string) (Session, string, error) {
	var u User
	var hash string
	err := s.Pool.QueryRow(ctx, `SELECT id,email,name,password_hash FROM users WHERE email=$1`, email).
		Scan(&u.ID, &u.Email, &u.Name, &hash)
	if errors.Is(err, pgx.ErrNoRows) || !CheckPassword(hash, password) {
		return Session{}, "", ErrNotFound
	}
	if err != nil {
		return Session{}, "", err
	}
	var orgID uuid.UUID
	var role string
	err = s.Pool.QueryRow(ctx, `SELECT organization_id, role FROM memberships WHERE user_id=$1 ORDER BY created_at ASC LIMIT 1`, u.ID).
		Scan(&orgID, &role)
	if err != nil {
		return Session{}, "", err
	}
	secret, prefix, th, err := auth.Mint("lst_sess")
	if err != nil {
		return Session{}, "", err
	}
	var sid uuid.UUID
	err = s.Pool.QueryRow(ctx, `
INSERT INTO sessions(user_id,token_hash,prefix,expires_at) VALUES($1,$2,$3,now() + interval '7 days')
RETURNING id`, u.ID, th, prefix).Scan(&sid)
	if err != nil {
		return Session{}, "", err
	}
	return Session{UserID: u.ID, Email: u.Email, Name: u.Name, OrganizationID: orgID, Role: role, SessionID: sid}, secret, nil
}

func (s *Store) AuthSession(ctx context.Context, secret string) (Session, error) {
	prefix := auth.PrefixOf(secret)
	var sid, uid, orgID uuid.UUID
	var hash []byte
	var email, name, role string
	err := s.Pool.QueryRow(ctx, `
SELECT s.id, s.user_id, s.token_hash, u.email, u.name, m.organization_id, m.role
FROM sessions s
JOIN users u ON u.id=s.user_id
JOIN memberships m ON m.user_id=u.id
WHERE s.prefix=$1 AND s.revoked_at IS NULL AND s.expires_at > now()
ORDER BY m.created_at ASC LIMIT 1`, prefix).
		Scan(&sid, &uid, &hash, &email, &name, &orgID, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	if !auth.Equal(hash, auth.Hash(secret)) {
		return Session{}, ErrNotFound
	}
	return Session{UserID: uid, Email: email, Name: name, OrganizationID: orgID, Role: role, SessionID: sid}, nil
}

func roleAtLeast(role, need string) bool {
	rank := map[string]int{"viewer": 1, "developer": 2, "maintainer": 3, "admin": 4, "owner": 5, "billing": 1}
	return rank[role] >= rank[need]
}

func (s Session) Can(need string) bool { return roleAtLeast(s.Role, need) }

func (s *Store) Audit(ctx context.Context, actorUser, actorToken *uuid.UUID, orgID, projectID *uuid.UUID, action, targetType, targetID, ip, ua string, meta any) {
	var raw json.RawMessage = json.RawMessage(`{}`)
	if meta != nil {
		if b, err := json.Marshal(meta); err == nil {
			raw = b
		}
	}
	_, _ = s.Pool.Exec(ctx, `
INSERT INTO audit_logs(actor_user_id,actor_token_id,organization_id,project_id,action,target_type,target_id,ip,user_agent,metadata)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, actorUser, actorToken, orgID, projectID, action, targetType, targetID, ip, ua, raw)
}

func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT a.id, a.action, a.target_type, a.target_id, a.metadata, a.created_at, COALESCE(u.email,'')
FROM audit_logs a LEFT JOIN users u ON u.id=a.actor_user_id
ORDER BY a.created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.Action, &e.Target, &e.TargetID, &e.Metadata, &e.CreatedAt, &e.Actor); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) CreateArtifact(ctx context.Context, projectID uuid.UUID, typ, buildID, arch, sha, key string, size int64) (Artifact, error) {
	var a Artifact
	err := s.Pool.QueryRow(ctx, `
INSERT INTO artifacts(project_id,type,build_id,architecture,size,sha256,object_key,status)
VALUES($1,$2,$3,$4,$5,$6,$7,'ready')
ON CONFLICT (project_id, sha256) DO UPDATE SET
  build_id=EXCLUDED.build_id, architecture=EXCLUDED.architecture, status='ready'
RETURNING id,build_id,sha256,object_key,architecture,status`,
		projectID, typ, buildID, arch, size, sha, key).
		Scan(&a.ID, &a.BuildID, &a.SHA256, &a.ObjectKey, &a.Architecture, &a.Status)
	return a, err
}

func (s *Store) ListArtifacts(ctx context.Context, projectID uuid.UUID, limit int) ([]Artifact, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,build_id,sha256,object_key,architecture,status FROM artifacts
WHERE project_id=$1 ORDER BY created_at DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		var a Artifact
		if err := rows.Scan(&a.ID, &a.BuildID, &a.SHA256, &a.ObjectKey, &a.Architecture, &a.Status); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetArtifact(ctx context.Context, id uuid.UUID) (Artifact, error) {
	var a Artifact
	err := s.Pool.QueryRow(ctx, `SELECT id,build_id,sha256,object_key,architecture,status FROM artifacts WHERE id=$1`, id).
		Scan(&a.ID, &a.BuildID, &a.SHA256, &a.ObjectKey, &a.Architecture, &a.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, ErrNotFound
	}
	return a, err
}

func (s *Store) ListReleases(ctx context.Context, projectID uuid.UUID, limit int) ([]Release, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,version,build_id,status FROM releases WHERE project_id=$1 ORDER BY introduced_at DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Release
	for rows.Next() {
		var r Release
		if err := rows.Scan(&r.ID, &r.Version, &r.BuildID, &r.Status); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetRelease(ctx context.Context, id uuid.UUID) (Release, error) {
	var r Release
	err := s.Pool.QueryRow(ctx, `SELECT id,version,build_id,status FROM releases WHERE id=$1`, id).
		Scan(&r.ID, &r.Version, &r.BuildID, &r.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Release{}, ErrNotFound
	}
	return r, err
}

func (s *Store) GetDevice(ctx context.Context, id uuid.UUID) (Device, error) {
	var d Device
	err := s.Pool.QueryRow(ctx, `
SELECT id,project_id,device_id,product,hardware_revision,firmware_version,build_id,status,first_seen,last_seen
FROM devices WHERE id=$1`, id).Scan(&d.ID, &d.ProjectID, &d.DeviceID, &d.Product, &d.HardwareRevision, &d.FirmwareVersion, &d.BuildID, &d.Status, &d.FirstSeen, &d.LastSeen)
	if errors.Is(err, pgx.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return d, err
}

func (s *Store) ListEventsByIssue(ctx context.Context, issueID uuid.UUID, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,event_id,project_id,device_id,release_id,artifact_id,issue_id,type,severity,architecture,sequence,source_event_id,
raw_object_key,raw_hash,size_bytes,state,fingerprint,decoded,analysis,frames,received_at,processed_at,duplicate_count
FROM events WHERE issue_id=$1 ORDER BY received_at DESC LIMIT $2`, issueID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *Store) ListEventsByDevice(ctx context.Context, deviceID uuid.UUID, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,event_id,project_id,device_id,release_id,artifact_id,issue_id,type,severity,architecture,sequence,source_event_id,
raw_object_key,raw_hash,size_bytes,state,fingerprint,decoded,analysis,frames,received_at,processed_at,duplicate_count
FROM events WHERE device_id=$1 ORDER BY received_at DESC LIMIT $2`, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *Store) UpdateIssueStatus(ctx context.Context, id uuid.UUID, status string) (Issue, error) {
	var i Issue
	err := s.Pool.QueryRow(ctx, `
UPDATE issues SET status=$2 WHERE id=$1
RETURNING id,project_id,fingerprint,title,status,severity,event_count,affected_devices,first_seen,last_seen,probable_cause`,
		id, status).Scan(&i.ID, &i.ProjectID, &i.Fingerprint, &i.Title, &i.Status, &i.Severity, &i.EventCount, &i.AffectedDevices, &i.FirstSeen, &i.LastSeen, &i.ProbableCause)
	if errors.Is(err, pgx.ErrNoRows) {
		return Issue{}, ErrNotFound
	}
	return i, err
}

func (s *Store) CreateProjectToken(ctx context.Context, projectID uuid.UUID, name string, scopes []string) (Token, string, error) {
	secret, prefix, hash, err := auth.Mint("lst_ingest")
	if err != nil {
		return Token{}, "", err
	}
	if len(scopes) == 0 {
		scopes = []string{"event:write", "event:read", "artifact:write"}
	}
	var t Token
	err = s.Pool.QueryRow(ctx, `
INSERT INTO tokens(project_id,name,prefix,hash,scopes) VALUES($1,$2,$3,$4,$5)
RETURNING id,project_id,name,prefix,scopes`, projectID, name, prefix, hash, scopes).
		Scan(&t.ID, &t.ProjectID, &t.Name, &t.Prefix, &t.Scopes)
	return t, secret, err
}

func scanEvents(rows pgx.Rows) ([]Event, error) {
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.EventID, &e.ProjectID, &e.DeviceID, &e.ReleaseID, &e.ArtifactID, &e.IssueID, &e.Type, &e.Severity,
			&e.Architecture, &e.Sequence, &e.SourceEventID, &e.RawObjectKey, &e.RawHash, &e.SizeBytes, &e.State, &e.Fingerprint,
			&e.Decoded, &e.Analysis, &e.Frames, &e.ReceivedAt, &e.ProcessedAt, &e.DuplicateCount); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func TokenHasScope(t Token, scope string) bool {
	for _, s := range t.Scopes {
		if s == scope || s == "project:admin" {
			return true
		}
	}
	return false
}
