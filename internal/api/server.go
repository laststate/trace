package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/artifact"
	"github.com/laststate/trace/internal/auth"
	"github.com/laststate/trace/internal/config"
	"github.com/laststate/trace/internal/lep"
	"github.com/laststate/trace/internal/metrics"
	"github.com/laststate/trace/internal/objects"
	"github.com/laststate/trace/internal/store"
)

type Server struct {
	Cfg    config.Config
	Store  *store.Store
	Object objects.Store
	Log    *slog.Logger
	UI     http.FileSystem
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", s.live)
	mux.HandleFunc("GET /health/ready", s.ready)
	mux.HandleFunc("GET /metrics", metrics.Handler)

	mux.HandleFunc("GET /v1/relay/capabilities", s.capabilities)
	mux.HandleFunc("POST /v1/ingest", s.ingest)
	mux.HandleFunc("POST /v1/events", s.ingest)
	mux.HandleFunc("POST /v1/events:batch", s.ingestBatch)
	mux.HandleFunc("POST /v1/artifacts", s.uploadArtifact)

	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("GET /api/me", s.me)
	mux.HandleFunc("GET /api/overview", s.requireUI(s.apiOverview, "viewer"))
	mux.HandleFunc("GET /api/issues", s.requireUI(s.apiIssues, "viewer"))
	mux.HandleFunc("GET /api/issues/{id}", s.requireUI(s.apiIssue, "viewer"))
	mux.HandleFunc("POST /api/issues/{id}/status", s.requireUI(s.apiIssueStatus, "developer"))
	mux.HandleFunc("GET /api/events", s.requireUI(s.apiEvents, "viewer"))
	mux.HandleFunc("GET /api/events/{id}", s.requireUI(s.apiEvent, "viewer"))
	mux.HandleFunc("GET /api/events/{id}/raw", s.requireUI(s.apiEventRaw, "viewer"))
	mux.HandleFunc("GET /api/devices", s.requireUI(s.apiDevices, "viewer"))
	mux.HandleFunc("GET /api/devices/{id}", s.requireUI(s.apiDevice, "viewer"))
	mux.HandleFunc("GET /api/releases", s.requireUI(s.apiReleases, "viewer"))
	mux.HandleFunc("GET /api/releases/{id}", s.requireUI(s.apiRelease, "viewer"))
	mux.HandleFunc("GET /api/artifacts", s.requireUI(s.apiArtifacts, "viewer"))
	mux.HandleFunc("POST /api/artifacts", s.requireUI(s.apiUploadArtifact, "maintainer"))
	mux.HandleFunc("GET /api/audit", s.requireUI(s.apiAudit, "admin"))
	mux.HandleFunc("POST /api/tokens", s.requireUI(s.apiCreateToken, "admin"))
	mux.HandleFunc("GET /api/bootstrap", s.apiBootstrapInfo)

	if s.UI != nil {
		fileServer := http.FileServer(s.UI)
		mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") || strings.HasPrefix(r.URL.Path, "/health/") || r.URL.Path == "/metrics" {
				http.NotFound(w, r)
				return
			}
			if r.URL.Path != "/" && !strings.Contains(r.URL.Path, ".") {
				r.URL.Path = "/"
			}
			fileServer.ServeHTTP(w, r)
		}))
	}
	return withRequestID(mux)
}

func (s *Server) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.Pool.Ping(r.Context()); err != nil {
		writeErr(w, 503, "not_ready", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ready"})
}

func (s *Server) capabilities(w http.ResponseWriter, _ *http.Request) {
	max := s.Cfg.MaxEventSize
	writeJSON(w, 200, map[string]any{
		"api_version": "1", "lep_versions": []int{1}, "max_event_size": max,
		"max_batch_events": 100, "max_batch_bytes": max * 50, "compression": []string{"identity"},
		"batch_ingest": true, "binary_batch": false, "artifact_upload": true,
		"authentication": []string{"bearer"}, "server_id": "trace-v0.2",
	})
}

func (s *Server) ingest(w http.ResponseWriter, r *http.Request) {
	metrics.IngestTotal.Add(1)
	tok, err := s.requireIngest(r)
	if err != nil {
		metrics.IngestRejected.Add(1)
		writeErr(w, 401, "unauthorized", err.Error(), false)
		return
	}
	body := http.MaxBytesReader(w, r.Body, s.Cfg.MaxEventSize)
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil {
		metrics.IngestRejected.Add(1)
		writeErr(w, 400, "invalid_body", "could not read body", false)
		return
	}
	eventID := r.Header.Get("X-Last-State-Event-ID")
	if eventID == "" {
		eventID = r.Header.Get("Idempotency-Key")
	}
	res, code, msg, retryable, err := s.acceptOne(r, tok.ProjectID, eventID, raw)
	if err != nil {
		metrics.IngestRejected.Add(1)
		status := 422
		if retryable {
			status = 503
		}
		if code == "too_large" {
			status = 413
		}
		writeErr(w, status, code, msg, retryable)
		return
	}
	if res.Duplicate {
		metrics.IngestDuplicate.Add(1)
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(res.Event.ID.String()))
		return
	}
	metrics.IngestAccepted.Add(1)
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(res.Event.ID.String()))
}

type batchRequest struct {
	Events []struct {
		EventID string `json:"event_id"`
		Payload []byte `json:"payload"`
	} `json:"events"`
}

func (s *Server) ingestBatch(w http.ResponseWriter, r *http.Request) {
	metrics.IngestTotal.Add(1)
	tok, err := s.requireIngest(r)
	if err != nil {
		writeErr(w, 401, "unauthorized", err.Error(), false)
		return
	}
	body := http.MaxBytesReader(w, r.Body, s.Cfg.MaxEventSize*100)
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil {
		writeErr(w, 400, "invalid_body", "could not read body", false)
		return
	}
	var req batchRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	resp := map[string]any{"accepted": []any{}, "duplicates": []any{}, "rejected": []any{}}
	var accepted, dups, rejected []map[string]any
	for _, e := range req.Events {
		res, code, msg, retryable, err := s.acceptOne(r, tok.ProjectID, e.EventID, e.Payload)
		if err != nil {
			metrics.IngestRejected.Add(1)
			rejected = append(rejected, map[string]any{"event_id": e.EventID, "code": code, "message": msg, "retryable": retryable})
			continue
		}
		item := map[string]any{"event_id": e.EventID, "receipt": res.Event.ID.String()}
		if res.Duplicate {
			metrics.IngestDuplicate.Add(1)
			dups = append(dups, item)
		} else {
			metrics.IngestAccepted.Add(1)
			accepted = append(accepted, item)
		}
	}
	resp["accepted"], resp["duplicates"], resp["rejected"] = accepted, dups, rejected
	writeJSON(w, 200, resp)
}

func (s *Server) acceptOne(r *http.Request, projectID uuid.UUID, eventID string, raw []byte) (store.IngestResult, string, string, bool, error) {
	if int64(len(raw)) > s.Cfg.MaxEventSize {
		return store.IngestResult{}, "too_large", "event exceeds max size", false, errors.New("too large")
	}
	h, err := lep.Validate(raw)
	if err != nil {
		var ve *lep.ValidationError
		if errors.As(err, &ve) {
			if ve.Kind == lep.ErrorTooLarge {
				return store.IngestResult{}, "too_large", ve.Error(), false, err
			}
			if ve.Kind == lep.ErrorUnsupported {
				return store.IngestResult{}, "unsupported", ve.Error(), false, err
			}
		}
		return store.IngestResult{}, "corrupt", err.Error(), false, err
	}
	key, hash, err := s.Object.Put(raw)
	if err != nil {
		return store.IngestResult{}, "storage_error", err.Error(), true, err
	}
	if eventID == "" {
		eventID = "evt_" + hash[:26]
	}
	severity := "error"
	if h.Type == lep.TypeCrash || h.Type == lep.TypeCoredump {
		severity = "fatal"
	}
	decoded, _ := json.Marshal(map[string]any{"header": h})
	res, err := s.Store.CreateEventIdempotent(r.Context(), projectID, eventID, int16(h.Type), int16(h.Architecture), int64(h.Sequence), int64(h.EventID), severity, key, hash, int64(len(raw)), decoded)
	if err != nil {
		return store.IngestResult{}, "internal", err.Error(), true, err
	}
	return res, "", "", false, nil
}

func (s *Server) uploadArtifact(w http.ResponseWriter, r *http.Request) {
	// bearer ingest/artifact token
	tok, err := s.requireIngest(r)
	if err != nil {
		writeErr(w, 401, "unauthorized", err.Error(), false)
		return
	}
	if !store.TokenHasScope(tok, "artifact:write") {
		writeErr(w, 403, "forbidden", "artifact:write required", false)
		return
	}
	a, err := s.saveArtifact(r, tok.ProjectID)
	if err != nil {
		writeErr(w, 422, "invalid_artifact", err.Error(), false)
		return
	}
	metrics.ArtifactUploads.Add(1)
	tid := tok.ID
	pid := tok.ProjectID
	s.Store.Audit(r.Context(), nil, &tid, nil, &pid, "artifact.upload", "artifact", a.ID.String(), clientIP(r), r.UserAgent(), map[string]any{"build_id": a.BuildID})
	writeJSON(w, 201, a)
}

func (s *Server) saveArtifact(r *http.Request, projectID uuid.UUID) (store.Artifact, error) {
	body := http.MaxBytesReader(nil, r.Body, s.Cfg.MaxArtifactSize)
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil {
		return store.Artifact{}, err
	}
	if len(raw) == 0 {
		return store.Artifact{}, errors.New("empty body")
	}
	key, hash, err := s.Object.Put(raw)
	if err != nil {
		return store.Artifact{}, err
	}
	tmp, err := artifact.WriteTemp(raw)
	if err != nil {
		return store.Artifact{}, err
	}
	defer os.Remove(tmp)
	meta, err := artifact.InspectFile(tmp)
	if err != nil {
		// allow non-ELF with explicit header build id
		if bid := r.Header.Get("X-Last-State-Build-ID"); bid != "" {
			meta.BuildID = strings.ToLower(bid)
			meta.Architecture = r.Header.Get("X-Last-State-Architecture")
		} else {
			return store.Artifact{}, err
		}
	}
	return s.Store.CreateArtifact(r.Context(), projectID, "elf", meta.BuildID, meta.Architecture, hash, key, int64(len(raw)))
}

func (s *Server) requireIngest(r *http.Request) (store.Token, error) {
	secret, err := auth.Bearer(r.Header.Get("Authorization"))
	if err != nil {
		return store.Token{}, err
	}
	return s.Store.AuthIngestToken(r.Context(), secret)
}

type ctxKey int

const sessKey ctxKey = 1

func (s *Server) requireUI(next http.HandlerFunc, minRole string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// OpenUI: GET is open for local/dev; mutating methods always need session.
		if s.Cfg.OpenUI && r.Method == http.MethodGet {
			next(w, r)
			return
		}
		secret, err := auth.Bearer(r.Header.Get("Authorization"))
		if err != nil {
			writeErr(w, 401, "unauthorized", "login required", false)
			return
		}
		sess, err := s.Store.AuthSession(r.Context(), secret)
		if err != nil {
			writeErr(w, 401, "unauthorized", "invalid session", false)
			return
		}
		if !sess.Can(minRole) {
			writeErr(w, 403, "forbidden", "insufficient role", false)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), sessKey, sess)))
	}
}

func (s *Server) project(r *http.Request) (store.Project, error) {
	return s.Store.DefaultProject(r.Context())
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	sess, secret, err := s.Store.Login(r.Context(), body.Email, body.Password)
	if err != nil {
		writeErr(w, 401, "invalid_credentials", "invalid email or password", false)
		return
	}
	uid := sess.UserID
	oid := sess.OrganizationID
	s.Store.Audit(r.Context(), &uid, nil, &oid, nil, "auth.login", "user", uid.String(), clientIP(r), r.UserAgent(), nil)
	writeJSON(w, 200, map[string]any{
		"token": secret,
		"user":  map[string]any{"id": sess.UserID, "email": sess.Email, "name": sess.Name, "role": sess.Role},
	})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	secret, err := auth.Bearer(r.Header.Get("Authorization"))
	if err != nil {
		writeErr(w, 401, "unauthorized", err.Error(), false)
		return
	}
	sess, err := s.Store.AuthSession(r.Context(), secret)
	if err != nil {
		writeErr(w, 401, "unauthorized", "invalid session", false)
		return
	}
	writeJSON(w, 200, map[string]any{"id": sess.UserID, "email": sess.Email, "name": sess.Name, "role": sess.Role})
}

func (s *Server) apiOverview(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	ov, err := s.Store.Overview(r.Context(), p.ID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	ov["project"] = p
	writeJSON(w, 200, ov)
}

func (s *Server) apiIssues(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	items, err := s.Store.ListIssues(r.Context(), p.ID, 100)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiIssue(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	item, err := s.Store.GetIssue(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	events, _ := s.Store.ListEventsByIssue(r.Context(), id, 50)
	writeJSON(w, 200, map[string]any{"issue": item, "events": events})
}

func (s *Server) apiIssueStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	switch body.Status {
	case "open", "investigating", "resolved", "ignored", "archived":
	default:
		writeErr(w, 400, "bad_status", "invalid status", false)
		return
	}
	item, err := s.Store.UpdateIssueStatus(r.Context(), id, body.Status)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	s.Store.Audit(r.Context(), nil, nil, nil, &item.ProjectID, "issue.status", "issue", id.String(), clientIP(r), r.UserAgent(), map[string]any{"status": body.Status})
	writeJSON(w, 200, item)
}

func (s *Server) apiEvents(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	items, err := s.Store.ListEvents(r.Context(), p.ID, 100)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiEvent(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	item, err := s.Store.GetEventByID(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) apiEventRaw(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	item, err := s.Store.GetEventByID(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	raw, err := s.Object.Get(item.RawObjectKey)
	if err != nil {
		writeErr(w, 500, "storage_error", err.Error(), true)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+item.EventID+".lep\"")
	_, _ = w.Write(raw)
}

func (s *Server) apiDevices(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	items, err := s.Store.ListDevices(r.Context(), p.ID, 100)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiDevice(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	d, err := s.Store.GetDevice(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	events, _ := s.Store.ListEventsByDevice(r.Context(), id, 50)
	writeJSON(w, 200, map[string]any{"device": d, "events": events})
}

func (s *Server) apiReleases(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	items, err := s.Store.ListReleases(r.Context(), p.ID, 100)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiRelease(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	item, err := s.Store.GetRelease(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) apiArtifacts(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	items, err := s.Store.ListArtifacts(r.Context(), p.ID, 100)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiUploadArtifact(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	a, err := s.saveArtifact(r, p.ID)
	if err != nil {
		writeErr(w, 422, "invalid_artifact", err.Error(), false)
		return
	}
	metrics.ArtifactUploads.Add(1)
	pid := p.ID
	s.Store.Audit(r.Context(), nil, nil, &p.OrganizationID, &pid, "artifact.upload", "artifact", a.ID.String(), clientIP(r), r.UserAgent(), map[string]any{"build_id": a.BuildID})
	writeJSON(w, 201, a)
}

func (s *Server) apiAudit(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListAudit(r.Context(), 100)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiCreateToken(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	var body struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	if body.Name == "" {
		body.Name = "api-token"
	}
	t, secret, err := s.Store.CreateProjectToken(r.Context(), p.ID, body.Name, body.Scopes)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	pid := p.ID
	s.Store.Audit(r.Context(), nil, nil, &p.OrganizationID, &pid, "token.create", "token", t.ID.String(), clientIP(r), r.UserAgent(), map[string]any{"name": body.Name})
	writeJSON(w, 201, map[string]any{"token": t, "secret": secret})
}

func (s *Server) apiBootstrapInfo(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeJSON(w, 200, map[string]any{"bootstrapped": false, "open_ui": s.Cfg.OpenUI})
		return
	}
	writeJSON(w, 200, map[string]any{"bootstrapped": true, "project": p, "open_ui": s.Cfg.OpenUI})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string, retryable bool) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code": code, "message": msg, "retryable": retryable,
			"request_id": w.Header().Get("X-Request-ID"),
		},
	})
}

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = auth.RandomHex(8)
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	if x := r.Header.Get("X-Forwarded-For"); x != "" {
		return strings.Split(x, ",")[0]
	}
	return r.RemoteAddr
}
