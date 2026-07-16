package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/auth"
	"github.com/laststate/trace/internal/config"
	"github.com/laststate/trace/internal/lep"
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
	mux.HandleFunc("GET /v1/relay/capabilities", s.capabilities)
	mux.HandleFunc("POST /v1/ingest", s.ingest)
	mux.HandleFunc("POST /v1/events", s.ingest) // alias
	mux.HandleFunc("POST /v1/events:batch", s.ingestBatch)

	// admin/UI API (token or open for local v0.1 when project default)
	mux.HandleFunc("GET /api/overview", s.apiOverview)
	mux.HandleFunc("GET /api/issues", s.apiIssues)
	mux.HandleFunc("GET /api/issues/{id}", s.apiIssue)
	mux.HandleFunc("GET /api/events", s.apiEvents)
	mux.HandleFunc("GET /api/events/{id}", s.apiEvent)
	mux.HandleFunc("GET /api/events/{id}/raw", s.apiEventRaw)
	mux.HandleFunc("GET /api/devices", s.apiDevices)
	mux.HandleFunc("GET /api/bootstrap", s.apiBootstrapInfo)

	if s.UI != nil {
		fileServer := http.FileServer(s.UI)
		mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") || strings.HasPrefix(r.URL.Path, "/health/") {
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

type capabilities struct {
	APIVersion     string   `json:"api_version"`
	LEPVersions    []int    `json:"lep_versions"`
	MaxEventSize   int64    `json:"max_event_size"`
	MaxBatchEvents int      `json:"max_batch_events"`
	MaxBatchBytes  int64    `json:"max_batch_bytes"`
	Compression    []string `json:"compression"`
	BatchIngest    bool     `json:"batch_ingest"`
	BinaryBatch    bool     `json:"binary_batch"`
	ArtifactUpload bool     `json:"artifact_upload"`
	Authentication []string `json:"authentication"`
	ServerID       string   `json:"server_id"`
}

func (s *Server) capabilities(w http.ResponseWriter, _ *http.Request) {
	max := s.Cfg.MaxEventSize
	writeJSON(w, 200, capabilities{
		APIVersion: "1", LEPVersions: []int{1}, MaxEventSize: max,
		MaxBatchEvents: 100, MaxBatchBytes: max * 50, Compression: []string{"identity"},
		BatchIngest: true, BinaryBatch: false, ArtifactUpload: false,
		Authentication: []string{"bearer"}, ServerID: "trace-v0.1",
	})
}

func (s *Server) ingest(w http.ResponseWriter, r *http.Request) {
	tok, err := s.requireIngest(r)
	if err != nil {
		writeErr(w, 401, "unauthorized", err.Error(), false)
		return
	}
	max := s.Cfg.MaxEventSize
	body := http.MaxBytesReader(w, r.Body, max)
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil {
		writeErr(w, 400, "invalid_body", "could not read body", false)
		return
	}
	eventID := r.Header.Get("X-Last-State-Event-ID")
	if eventID == "" {
		eventID = r.Header.Get("Idempotency-Key")
	}
	res, code, msg, retryable, err := s.acceptOne(r, tok.ProjectID, eventID, raw)
	if err != nil {
		status := 422
		if retryable {
			status = 503
		}
		if code == "too_large" {
			status = 413
		}
		if code == "duplicate" {
			// shouldn't happen via err
		}
		writeErr(w, status, code, msg, retryable)
		return
	}
	if res.Duplicate {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(res.Event.ID.String()))
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(res.Event.ID.String()))
}

type batchRequest struct {
	Events []struct {
		EventID string `json:"event_id"`
		Payload []byte `json:"payload"`
	} `json:"events"`
}

type batchResponse struct {
	Accepted   []accepted `json:"accepted,omitempty"`
	Duplicates []accepted `json:"duplicates,omitempty"`
	Rejected   []rejected `json:"rejected,omitempty"`
}
type accepted struct {
	EventID string `json:"event_id"`
	Receipt string `json:"receipt,omitempty"`
}
type rejected struct {
	EventID   string `json:"event_id"`
	Code      string `json:"code"`
	Message   string `json:"message,omitempty"`
	Retryable bool   `json:"retryable"`
}

func (s *Server) ingestBatch(w http.ResponseWriter, r *http.Request) {
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
	var resp batchResponse
	for _, e := range req.Events {
		res, code, msg, retryable, err := s.acceptOne(r, tok.ProjectID, e.EventID, e.Payload)
		if err != nil {
			resp.Rejected = append(resp.Rejected, rejected{EventID: e.EventID, Code: code, Message: msg, Retryable: retryable})
			continue
		}
		item := accepted{EventID: e.EventID, Receipt: res.Event.ID.String()}
		if res.Duplicate {
			resp.Duplicates = append(resp.Duplicates, item)
		} else {
			resp.Accepted = append(resp.Accepted, item)
		}
	}
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
	decoded, _ := json.Marshal(map[string]any{
		"header": h,
	})
	res, err := s.Store.CreateEventIdempotent(r.Context(), projectID, eventID, int16(h.Type), int16(h.Architecture), int64(h.Sequence), int64(h.EventID), severity, key, hash, int64(len(raw)), decoded)
	if err != nil {
		return store.IngestResult{}, "internal", err.Error(), true, err
	}
	return res, "", "", false, nil
}

func (s *Server) requireIngest(r *http.Request) (store.Token, error) {
	secret, err := auth.Bearer(r.Header.Get("Authorization"))
	if err != nil {
		return store.Token{}, err
	}
	return s.Store.AuthIngestToken(r.Context(), secret)
}

func (s *Server) project(r *http.Request) (store.Project, error) {
	// v0.1: single default project for UI
	return s.Store.DefaultProject(r.Context())
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

func (s *Server) apiBootstrapInfo(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeJSON(w, 200, map[string]any{"bootstrapped": false})
		return
	}
	writeJSON(w, 200, map[string]any{"bootstrapped": true, "project": p})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string, retryable bool) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":       code,
			"message":    msg,
			"retryable":  retryable,
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

