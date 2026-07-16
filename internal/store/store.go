package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laststate/trace/internal/auth"
)

var ErrNotFound = errors.New("not found")
var ErrDuplicate = errors.New("duplicate")

type Store struct {
	Pool *pgxpool.Pool
}

type Org struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Slug string    `json:"slug"`
}

type Project struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Name           string    `json:"name"`
	Slug           string    `json:"slug"`
	Description    string    `json:"description"`
}

type Token struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"project_id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	Scopes    []string  `json:"scopes"`
}

type Device struct {
	ID               uuid.UUID `json:"id"`
	ProjectID        uuid.UUID `json:"project_id"`
	DeviceID         string    `json:"device_id"`
	Product          string    `json:"product"`
	HardwareRevision string    `json:"hardware_revision"`
	FirmwareVersion  string    `json:"firmware_version"`
	BuildID          string    `json:"build_id"`
	Status           string    `json:"status"`
	FirstSeen        time.Time `json:"first_seen"`
	LastSeen         time.Time `json:"last_seen"`
}

type Release struct {
	ID      uuid.UUID `json:"id"`
	Version string    `json:"version"`
	BuildID string    `json:"build_id"`
	Status  string    `json:"status"`
}

type Artifact struct {
	ID           uuid.UUID `json:"id"`
	BuildID      string    `json:"build_id"`
	SHA256       string    `json:"sha256"`
	ObjectKey    string    `json:"object_key"`
	Architecture string    `json:"architecture"`
	Status       string    `json:"status"`
}

type Issue struct {
	ID              uuid.UUID `json:"id"`
	ProjectID       uuid.UUID `json:"project_id"`
	Fingerprint     string    `json:"fingerprint"`
	Title           string    `json:"title"`
	Status          string    `json:"status"`
	Severity        string    `json:"severity"`
	EventCount      int64     `json:"event_count"`
	AffectedDevices int64     `json:"affected_devices"`
	FirstSeen       time.Time `json:"first_seen"`
	LastSeen        time.Time `json:"last_seen"`
	ProbableCause   string    `json:"probable_cause"`
}

type Event struct {
	ID            uuid.UUID       `json:"id"`
	EventID       string          `json:"event_id"`
	ProjectID     uuid.UUID       `json:"project_id"`
	DeviceID      *uuid.UUID      `json:"device_id,omitempty"`
	ReleaseID     *uuid.UUID      `json:"release_id,omitempty"`
	ArtifactID    *uuid.UUID      `json:"artifact_id,omitempty"`
	IssueID       *uuid.UUID      `json:"issue_id,omitempty"`
	Type          int16           `json:"type"`
	Severity      string          `json:"severity"`
	Architecture  int16           `json:"architecture"`
	Sequence      int64           `json:"sequence"`
	SourceEventID int64           `json:"source_event_id"`
	RawObjectKey  string          `json:"raw_object_key"`
	RawHash       string          `json:"raw_hash"`
	SizeBytes     int64           `json:"size_bytes"`
	State         string          `json:"state"`
	Fingerprint   string          `json:"fingerprint"`
	Decoded       json.RawMessage `json:"decoded"`
	Analysis      json.RawMessage `json:"analysis"`
	Frames        json.RawMessage `json:"frames"`
	ReceivedAt    time.Time       `json:"received_at"`
	ProcessedAt   *time.Time      `json:"processed_at,omitempty"`
	DuplicateCount int64          `json:"duplicate_count"`
}

type IngestResult struct {
	Event     Event
	Duplicate bool
}

func (s *Store) Bootstrap(ctx context.Context) (org Org, project Project, tokenSecret string, err error) {
	var n int
	if err = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM organizations`).Scan(&n); err != nil {
		return
	}
	if n > 0 {
		err = fmt.Errorf("already bootstrapped")
		return
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)

	if err = tx.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES('Default','default') RETURNING id,name,slug`).
		Scan(&org.ID, &org.Name, &org.Slug); err != nil {
		return
	}
	if err = tx.QueryRow(ctx, `INSERT INTO projects(organization_id,name,slug,description) VALUES($1,'Default','default','Bootstrapped project') RETURNING id,organization_id,name,slug,description`,
		org.ID).Scan(&project.ID, &project.OrganizationID, &project.Name, &project.Slug, &project.Description); err != nil {
		return
	}
	secret, prefix, hash, err := auth.Mint("lst_ingest")
	if err != nil {
		return
	}
	var tokID uuid.UUID
	if err = tx.QueryRow(ctx, `INSERT INTO tokens(project_id,name,prefix,hash,scopes) VALUES($1,'default-ingest',$2,$3,$4) RETURNING id`,
		project.ID, prefix, hash, []string{"event:write", "event:read", "artifact:write"}).Scan(&tokID); err != nil {
		return
	}
	if err = tx.Commit(ctx); err != nil {
		return
	}
	tokenSecret = secret
	return
}

func (s *Store) AuthIngestToken(ctx context.Context, secret string) (Token, error) {
	prefix := auth.PrefixOf(secret)
	var t Token
	var hash []byte
	err := s.Pool.QueryRow(ctx, `
SELECT id, project_id, name, prefix, scopes, hash
FROM tokens WHERE prefix=$1 AND revoked_at IS NULL`, prefix).
		Scan(&t.ID, &t.ProjectID, &t.Name, &t.Prefix, &t.Scopes, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Token{}, ErrNotFound
	}
	if err != nil {
		return Token{}, err
	}
	if !auth.Equal(hash, auth.Hash(secret)) {
		return Token{}, ErrNotFound
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE tokens SET last_used_at=now() WHERE id=$1`, t.ID)
	return t, nil
}

func (s *Store) CreateEventIdempotent(ctx context.Context, projectID uuid.UUID, eventID string, headerType, arch int16, sequence, sourceEventID int64, severity, rawKey, rawHash string, size int64, decoded json.RawMessage) (IngestResult, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return IngestResult{}, err
	}
	defer tx.Rollback(ctx)

	var existing Event
	err = tx.QueryRow(ctx, `
SELECT id,event_id,project_id,type,severity,architecture,sequence,source_event_id,raw_object_key,raw_hash,size_bytes,state,fingerprint,decoded,analysis,frames,received_at,duplicate_count
FROM events WHERE project_id=$1 AND event_id=$2 FOR UPDATE`, projectID, eventID).Scan(
		&existing.ID, &existing.EventID, &existing.ProjectID, &existing.Type, &existing.Severity, &existing.Architecture,
		&existing.Sequence, &existing.SourceEventID, &existing.RawObjectKey, &existing.RawHash, &existing.SizeBytes,
		&existing.State, &existing.Fingerprint, &existing.Decoded, &existing.Analysis, &existing.Frames, &existing.ReceivedAt, &existing.DuplicateCount,
	)
	if err == nil {
		_, _ = tx.Exec(ctx, `UPDATE events SET duplicate_count=duplicate_count+1 WHERE id=$1`, existing.ID)
		existing.DuplicateCount++
		if err := tx.Commit(ctx); err != nil {
			return IngestResult{}, err
		}
		return IngestResult{Event: existing, Duplicate: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return IngestResult{}, err
	}

	var e Event
	if decoded == nil {
		decoded = json.RawMessage(`{}`)
	}
	err = tx.QueryRow(ctx, `
INSERT INTO events(event_id,project_id,type,severity,architecture,sequence,source_event_id,raw_object_key,raw_hash,size_bytes,state,decoded,analysis,frames)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'persisted',$11,'{}'::jsonb,'[]'::jsonb)
RETURNING id,event_id,project_id,type,severity,architecture,sequence,source_event_id,raw_object_key,raw_hash,size_bytes,state,fingerprint,decoded,analysis,frames,received_at,duplicate_count`,
		eventID, projectID, headerType, severity, arch, sequence, sourceEventID, rawKey, rawHash, size, decoded,
	).Scan(&e.ID, &e.EventID, &e.ProjectID, &e.Type, &e.Severity, &e.Architecture, &e.Sequence, &e.SourceEventID,
		&e.RawObjectKey, &e.RawHash, &e.SizeBytes, &e.State, &e.Fingerprint, &e.Decoded, &e.Analysis, &e.Frames, &e.ReceivedAt, &e.DuplicateCount)
	if err != nil {
		return IngestResult{}, err
	}
	// enqueue process job in same tx
	payload, _ := json.Marshal(map[string]string{"event_id": e.ID.String()})
	if _, err := tx.Exec(ctx, `INSERT INTO jobs(type,payload,status) VALUES('process_event',$1,'ready')`, payload); err != nil {
		return IngestResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IngestResult{}, err
	}
	return IngestResult{Event: e, Duplicate: false}, nil
}

func (s *Store) GetEventByID(ctx context.Context, id uuid.UUID) (Event, error) {
	var e Event
	err := s.Pool.QueryRow(ctx, `
SELECT id,event_id,project_id,device_id,release_id,artifact_id,issue_id,type,severity,architecture,sequence,source_event_id,
raw_object_key,raw_hash,size_bytes,state,fingerprint,decoded,analysis,frames,received_at,processed_at,duplicate_count
FROM events WHERE id=$1`, id).Scan(
		&e.ID, &e.EventID, &e.ProjectID, &e.DeviceID, &e.ReleaseID, &e.ArtifactID, &e.IssueID, &e.Type, &e.Severity,
		&e.Architecture, &e.Sequence, &e.SourceEventID, &e.RawObjectKey, &e.RawHash, &e.SizeBytes, &e.State, &e.Fingerprint,
		&e.Decoded, &e.Analysis, &e.Frames, &e.ReceivedAt, &e.ProcessedAt, &e.DuplicateCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	return e, err
}

func (s *Store) GetEventByEventID(ctx context.Context, projectID uuid.UUID, eventID string) (Event, error) {
	var e Event
	err := s.Pool.QueryRow(ctx, `
SELECT id,event_id,project_id,device_id,release_id,artifact_id,issue_id,type,severity,architecture,sequence,source_event_id,
raw_object_key,raw_hash,size_bytes,state,fingerprint,decoded,analysis,frames,received_at,processed_at,duplicate_count
FROM events WHERE project_id=$1 AND event_id=$2`, projectID, eventID).Scan(
		&e.ID, &e.EventID, &e.ProjectID, &e.DeviceID, &e.ReleaseID, &e.ArtifactID, &e.IssueID, &e.Type, &e.Severity,
		&e.Architecture, &e.Sequence, &e.SourceEventID, &e.RawObjectKey, &e.RawHash, &e.SizeBytes, &e.State, &e.Fingerprint,
		&e.Decoded, &e.Analysis, &e.Frames, &e.ReceivedAt, &e.ProcessedAt, &e.DuplicateCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	return e, err
}

func (s *Store) ListEvents(ctx context.Context, projectID uuid.UUID, limit int) ([]Event, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,event_id,project_id,device_id,release_id,artifact_id,issue_id,type,severity,architecture,sequence,source_event_id,
raw_object_key,raw_hash,size_bytes,state,fingerprint,decoded,analysis,frames,received_at,processed_at,duplicate_count
FROM events WHERE project_id=$1 ORDER BY received_at DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
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

func (s *Store) UpsertDevice(ctx context.Context, projectID uuid.UUID, deviceID, product, hw, fw, buildID string) (Device, error) {
	if deviceID == "" {
		deviceID = "unknown"
	}
	var d Device
	err := s.Pool.QueryRow(ctx, `
INSERT INTO devices(project_id,device_id,product,hardware_revision,firmware_version,build_id,status,last_seen,last_event_at)
VALUES($1,$2,$3,$4,$5,$6,'healthy',now(),now())
ON CONFLICT (project_id, device_id) DO UPDATE SET
  product=CASE WHEN EXCLUDED.product<>'' THEN EXCLUDED.product ELSE devices.product END,
  hardware_revision=CASE WHEN EXCLUDED.hardware_revision<>'' THEN EXCLUDED.hardware_revision ELSE devices.hardware_revision END,
  firmware_version=CASE WHEN EXCLUDED.firmware_version<>'' THEN EXCLUDED.firmware_version ELSE devices.firmware_version END,
  build_id=CASE WHEN EXCLUDED.build_id<>'' THEN EXCLUDED.build_id ELSE devices.build_id END,
  last_seen=now(), last_event_at=now(),
  status=CASE WHEN devices.status IN ('retired') THEN devices.status ELSE 'healthy' END
RETURNING id,project_id,device_id,product,hardware_revision,firmware_version,build_id,status,first_seen,last_seen`,
		projectID, deviceID, product, hw, fw, buildID,
	).Scan(&d.ID, &d.ProjectID, &d.DeviceID, &d.Product, &d.HardwareRevision, &d.FirmwareVersion, &d.BuildID, &d.Status, &d.FirstSeen, &d.LastSeen)
	return d, err
}

func (s *Store) UpsertRelease(ctx context.Context, projectID uuid.UUID, version, buildID string) (Release, error) {
	if buildID == "" && version == "" {
		return Release{}, nil
	}
	if buildID == "" {
		buildID = "version:" + version
	}
	if version == "" {
		version = buildID
	}
	var r Release
	err := s.Pool.QueryRow(ctx, `
INSERT INTO releases(project_id,version,build_id,status) VALUES($1,$2,$3,'active')
ON CONFLICT (project_id, build_id) DO UPDATE SET version=CASE WHEN EXCLUDED.version<>'' THEN EXCLUDED.version ELSE releases.version END
RETURNING id,version,build_id,status`, projectID, version, buildID).
		Scan(&r.ID, &r.Version, &r.BuildID, &r.Status)
	return r, err
}

func (s *Store) FindArtifactByBuildID(ctx context.Context, projectID uuid.UUID, buildID string) (Artifact, error) {
	var a Artifact
	err := s.Pool.QueryRow(ctx, `
SELECT id,build_id,sha256,object_key,architecture,status FROM artifacts
WHERE project_id=$1 AND lower(build_id)=lower($2) AND status='ready' ORDER BY created_at DESC LIMIT 1`,
		projectID, buildID).Scan(&a.ID, &a.BuildID, &a.SHA256, &a.ObjectKey, &a.Architecture, &a.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, ErrNotFound
	}
	return a, err
}

func (s *Store) UpsertIssue(ctx context.Context, projectID uuid.UUID, fp, title, severity, probable string, deviceID *uuid.UUID) (Issue, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Issue{}, err
	}
	defer tx.Rollback(ctx)

	var issue Issue
	err = tx.QueryRow(ctx, `
INSERT INTO issues(project_id,fingerprint,title,status,severity,event_count,affected_devices,probable_cause)
VALUES($1,$2,$3,'open',$4,1,0,$5)
ON CONFLICT (project_id, fingerprint) DO UPDATE SET
  event_count=issues.event_count+1,
  last_seen=now(),
  severity=CASE WHEN EXCLUDED.severity='fatal' THEN 'fatal' ELSE issues.severity END,
  probable_cause=CASE WHEN issues.probable_cause='' THEN EXCLUDED.probable_cause ELSE issues.probable_cause END,
  title=CASE WHEN issues.title='' THEN EXCLUDED.title ELSE issues.title END
RETURNING id,project_id,fingerprint,title,status,severity,event_count,affected_devices,first_seen,last_seen,probable_cause`,
		projectID, fp, title, severity, probable,
	).Scan(&issue.ID, &issue.ProjectID, &issue.Fingerprint, &issue.Title, &issue.Status, &issue.Severity,
		&issue.EventCount, &issue.AffectedDevices, &issue.FirstSeen, &issue.LastSeen, &issue.ProbableCause)
	if err != nil {
		return Issue{}, err
	}
	if deviceID != nil {
		// recompute affected devices cheaply
		var n int64
		_ = tx.QueryRow(ctx, `SELECT COUNT(DISTINCT device_id) FROM events WHERE issue_id=$1 AND device_id IS NOT NULL`, issue.ID).Scan(&n)
		// include this device even before event link
		_, _ = tx.Exec(ctx, `UPDATE issues SET affected_devices=GREATEST($2, (SELECT COUNT(DISTINCT device_id) FROM events WHERE issue_id=$1 AND device_id IS NOT NULL)) WHERE id=$1`, issue.ID, n)
	}
	if err := tx.Commit(ctx); err != nil {
		return Issue{}, err
	}
	return issue, nil
}

func (s *Store) FinalizeEvent(ctx context.Context, eventID uuid.UUID, deviceID, releaseID, artifactID, issueID *uuid.UUID, state, fp string, decoded, analysis, frames json.RawMessage) error {
	_, err := s.Pool.Exec(ctx, `
UPDATE events SET device_id=$2, release_id=$3, artifact_id=$4, issue_id=$5, state=$6, fingerprint=$7,
  decoded=$8, analysis=$9, frames=$10, processed_at=now()
WHERE id=$1`, eventID, deviceID, releaseID, artifactID, issueID, state, fp, decoded, analysis, frames)
	if err != nil {
		return err
	}
	if issueID != nil {
		_, _ = s.Pool.Exec(ctx, `
UPDATE issues SET affected_devices=(SELECT COUNT(DISTINCT device_id) FROM events WHERE issue_id=$1 AND device_id IS NOT NULL)
WHERE id=$1`, *issueID)
	}
	return nil
}

func (s *Store) ListIssues(ctx context.Context, projectID uuid.UUID, limit int) ([]Issue, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,project_id,fingerprint,title,status,severity,event_count,affected_devices,first_seen,last_seen,probable_cause
FROM issues WHERE project_id=$1 ORDER BY last_seen DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Issue
	for rows.Next() {
		var i Issue
		if err := rows.Scan(&i.ID, &i.ProjectID, &i.Fingerprint, &i.Title, &i.Status, &i.Severity, &i.EventCount, &i.AffectedDevices, &i.FirstSeen, &i.LastSeen, &i.ProbableCause); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) GetIssue(ctx context.Context, id uuid.UUID) (Issue, error) {
	var i Issue
	err := s.Pool.QueryRow(ctx, `
SELECT id,project_id,fingerprint,title,status,severity,event_count,affected_devices,first_seen,last_seen,probable_cause
FROM issues WHERE id=$1`, id).Scan(&i.ID, &i.ProjectID, &i.Fingerprint, &i.Title, &i.Status, &i.Severity, &i.EventCount, &i.AffectedDevices, &i.FirstSeen, &i.LastSeen, &i.ProbableCause)
	if errors.Is(err, pgx.ErrNoRows) {
		return Issue{}, ErrNotFound
	}
	return i, err
}

func (s *Store) ListDevices(ctx context.Context, projectID uuid.UUID, limit int) ([]Device, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,project_id,device_id,product,hardware_revision,firmware_version,build_id,status,first_seen,last_seen
FROM devices WHERE project_id=$1 ORDER BY last_seen DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.DeviceID, &d.Product, &d.HardwareRevision, &d.FirmwareVersion, &d.BuildID, &d.Status, &d.FirstSeen, &d.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) DefaultProject(ctx context.Context) (Project, error) {
	var p Project
	err := s.Pool.QueryRow(ctx, `SELECT id,organization_id,name,slug,description FROM projects ORDER BY created_at ASC LIMIT 1`).
		Scan(&p.ID, &p.OrganizationID, &p.Name, &p.Slug, &p.Description)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return p, err
}

func (s *Store) Overview(ctx context.Context, projectID uuid.UUID) (map[string]any, error) {
	var devices, openIssues, eventsToday int64
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM devices WHERE project_id=$1`, projectID).Scan(&devices)
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM issues WHERE project_id=$1 AND status='open'`, projectID).Scan(&openIssues)
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE project_id=$1 AND received_at > now() - interval '24 hours'`, projectID).Scan(&eventsToday)
	return map[string]any{
		"devices":      devices,
		"open_issues":  openIssues,
		"events_today": eventsToday,
	}, nil
}
