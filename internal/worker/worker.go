package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/laststate/trace/internal/analysis"
	"github.com/laststate/trace/internal/decode"
	"github.com/laststate/trace/internal/fingerprint"
	"github.com/laststate/trace/internal/lep"
	"github.com/laststate/trace/internal/metrics"
	"github.com/laststate/trace/internal/notify"
	"github.com/laststate/trace/internal/objects"
	"github.com/laststate/trace/internal/queue"
	"github.com/laststate/trace/internal/store"
	"github.com/laststate/trace/internal/symbolicate"
)

const SymbolizerVersion = 2

type Worker struct {
	Store  *store.Store
	Queue  queue.Jober
	Object *objects.Store
	Lease  time.Duration
	Log    *slog.Logger
}

func (w *Worker) Run(ctx context.Context) {
	if w.Log == nil {
		w.Log = slog.Default()
	}
	if w.Lease <= 0 {
		w.Lease = 30 * time.Second
	}
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

// ProcessEventOnce runs process_event for tests and admin tools.
func (w *Worker) ProcessEventOnce(ctx context.Context, eventID uuid.UUID) error {
	return w.processEvent(ctx, eventID, nil)
}

// HandleJob exposes job handling for tests.
func (w *Worker) HandleJob(ctx context.Context, job queue.Job) error {
	return w.handle(ctx, job)
}

func (w *Worker) tick(ctx context.Context) {
	job, err := w.Queue.Claim(ctx, w.Lease)
	if errors.Is(err, pgx.ErrNoRows) {
		return
	}
	if err != nil {
		w.Log.Error("claim job", "err", err)
		return
	}

	done := make(chan struct{})
	go func() {
		ren := time.NewTicker(w.Lease / 3)
		defer ren.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ren.C:
				if err := w.Queue.Renew(ctx, job.ID, job.Lease, w.Lease); err != nil {
					w.Log.Error("lease renew failed — aborting work to avoid split brain", "job", job.ID, "err", err)
					return
				}
			}
		}
	}()

	err = w.handle(ctx, job)
	close(done)

	if err != nil {
		metrics.JobsFailed.Add(1)
		w.Log.Error("job failed", "id", job.ID, "type", job.Type, "err", err)
		if ferr := w.Queue.Fail(ctx, job.ID, job.Lease, err.Error(), time.Duration(job.Attempt+1)*time.Second, 20); ferr != nil {
			w.Log.Error("queue fail", "job", job.ID, "err", ferr)
		}
		return
	}
	metrics.JobsCompleted.Add(1)
	if cerr := w.Queue.Complete(ctx, job.ID, job.Lease); cerr != nil {
		w.Log.Error("queue complete", "job", job.ID, "err", cerr)
	}
}

func (w *Worker) fireEscalationLevel(ctx context.Context, projectID, issueID, policyID uuid.UUID, level int, lvl map[string]any, payload map[string]any) {
	if urls, ok := lvl["webhooks"].([]any); ok {
		for _, u := range urls {
			url, _ := u.(string)
			if url == "" {
				continue
			}
			res, _ := notify.DeliverWithRetry(ctx, "webhook", url, "", nil, payload, notify.Options{MaxAttempts: 3})
			pid := policyID
			w.Store.LogEscalationFire(ctx, projectID, &issueID, &pid, level, url, res.Success)
		}
	}
	if emails, ok := lvl["emails"].([]any); ok {
		for _, e := range emails {
			em, _ := e.(string)
			if em == "" {
				continue
			}
			cfg := map[string]any{"to": em}
			res, _ := notify.DeliverWithRetry(ctx, "email", "", "", cfg, payload, notify.Options{MaxAttempts: 2})
			pid := policyID
			w.Store.LogEscalationFire(ctx, projectID, &issueID, &pid, level, "email:"+em, res.Success)
		}
	}
}

func (w *Worker) handle(ctx context.Context, job queue.Job) error {
	switch job.Type {
	case "notify_escalation":
		var p struct {
			ProjectID string         `json:"project_id"`
			IssueID   string         `json:"issue_id"`
			PolicyID  string         `json:"policy_id"`
			Level     int            `json:"level"`
			LevelCfg  map[string]any `json:"level_cfg"`
			Payload   map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return err
		}
		pid, _ := uuid.Parse(p.ProjectID)
		iid, _ := uuid.Parse(p.IssueID)
		pol, _ := uuid.Parse(p.PolicyID)
		w.fireEscalationLevel(ctx, pid, iid, pol, p.Level, p.LevelCfg, p.Payload)
		return nil
	case "process_event":
		var p struct {
			EventID   string `json:"event_id"`
			ProjectID string `json:"project_id"`
		}
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return err
		}
		id, err := uuid.Parse(p.EventID)
		if err != nil {
			return err
		}
		var wantProject *uuid.UUID
		if p.ProjectID != "" {
			pid, err := uuid.Parse(p.ProjectID)
			if err != nil {
				return err
			}
			wantProject = &pid
		}
		return w.processEvent(ctx, id, wantProject)
	case "notify_issue":
		var p struct {
			ProjectID string `json:"project_id"`
			IssueID   string `json:"issue_id"`
			Kind      string `json:"kind"`
			Dedupe    string `json:"dedupe"`
		}
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return err
		}
		pid, err := uuid.Parse(p.ProjectID)
		if err != nil {
			return err
		}
		iid, err := uuid.Parse(p.IssueID)
		if err != nil {
			return err
		}
		key := fmt.Sprintf("notify:%s:%s:%s:%s", pid, iid, p.Kind, p.Dedupe)
		first, err := w.Store.TryDedupeJob(ctx, key, "notify_issue")
		if err != nil {
			return err
		}
		if !first {
			w.Store.LogAlertSkip(ctx, pid, &iid, nil, p.Kind, "dedupe", key)
			return nil
		}
		return w.notifyIssue(ctx, pid, iid, p.Kind)
	case "reprocess_stale":
		var p struct {
			ProjectID string `json:"project_id"`
		}
		_ = json.Unmarshal(job.Payload, &p)
		return nil // batch reprocess is API-driven; reserved job type
	default:
		return errors.New("unknown job type: " + job.Type)
	}
}

func (w *Worker) processEvent(ctx context.Context, id uuid.UUID, wantProject *uuid.UUID) error {
	done, err := w.Store.MarkEventProcessing(ctx, id)
	if err != nil {
		return err
	}
	if done {
		return nil
	}

	ev, err := w.Store.GetEventByID(ctx, id)
	if err != nil {
		return err
	}
	// Multi-tenant: job project_id must match event.project_id when provided.
	if wantProject != nil && ev.ProjectID != *wantProject {
		return fmt.Errorf("project_id mismatch: job=%s event=%s", wantProject, ev.ProjectID)
	}

	raw, err := w.Object.Get(ev.RawObjectKey)
	if err != nil {
		return err
	}
	dec, err := decode.Envelope(raw)
	if err != nil {
		fail, _ := json.Marshal(map[string]string{"error": err.Error()})
		return w.Store.FinalizeEvent(ctx, id, nil, nil, nil, nil, "failed", "", fail, json.RawMessage(`{}`), json.RawMessage(`[]`))
	}

	pipeline := ev.Pipeline
	if pipeline == "" {
		pipeline = pipelineForType(dec.Header.Type)
	}

	// Non-issue pipelines
	switch pipeline {
	case "health", "log", "metric", "boot":
		return w.processTelemetry(ctx, ev, dec, pipeline)
	}

	return w.processIssueEvent(ctx, ev, dec)
}

func pipelineForType(t uint8) string {
	switch t {
	case lep.TypeHealth:
		return "health"
	case lep.TypeLog, lep.TypeMessage:
		return "log"
	case lep.TypePeripheral:
		return "metric"
	case lep.TypeReset:
		return "boot"
	default:
		return "issue"
	}
}

func (w *Worker) processTelemetry(ctx context.Context, ev store.Event, dec decode.Decoded, pipeline string) error {
	severity := "info"
	if pipeline == "health" {
		severity = "health"
	}
	device, err := w.Store.UpsertDevice(ctx, ev.ProjectID, dec.Identity.DeviceID, dec.Identity.Product, dec.Identity.HardwareRevision, dec.Identity.FirmwareVersion, dec.Identity.BuildID, severity)
	if err != nil {
		return err
	}
	did := device.ID
	_ = w.Store.MarkDeviceHealth(ctx, did, severity)
	_ = w.Store.RecordFirmwareHistory(ctx, ev.ProjectID, did, dec.Identity.FirmwareVersion, dec.Identity.BuildID)
	if dec.BootID != "" || dec.Identity.BootID != "" {
		boot := dec.BootID
		if boot == "" {
			boot = dec.Identity.BootID
		}
		_ = w.Store.UpsertBootSession(ctx, ev.ProjectID, &did, boot)
	}

	eid := ev.ID
	switch pipeline {
	case "health":
		_ = w.Store.InsertHealthSample(ctx, ev.ProjectID, &did, &eid, map[string]any{"identity": dec.Identity, "header": dec.Header})
	case "log":
		msg := dec.Assert
		if msg == "" {
			msg = lep.EventTypeName(dec.Header.Type)
		}
		_ = w.Store.InsertLogEntry(ctx, ev.ProjectID, &did, &eid, "info", msg)
	case "metric":
		_ = w.Store.InsertMetric(ctx, ev.ProjectID, &did, &eid, "peripheral", float64(dec.Header.Sequence))
	case "boot":
		_ = w.Store.UpsertBootSession(ctx, ev.ProjectID, &did, fmt.Sprintf("reset-%d", dec.Header.Sequence))
	}

	decodedJSON, _ := json.Marshal(dec)
	return w.Store.FinalizeEvent(ctx, ev.ID, &did, nil, nil, nil, "ready", "", decodedJSON, json.RawMessage(`{}`), json.RawMessage(`[]`))
}

func (w *Worker) processIssueEvent(ctx context.Context, ev store.Event, dec decode.Decoded) error {
	report := analysis.FromDecoded(dec)

	severity := "error"
	if dec.Event != nil {
		severity = decode.SeverityName(dec.Event.Severity)
	} else if dec.Header.Type == lep.TypeCrash || dec.Header.Type == lep.TypeCoredump {
		severity = "fatal"
	}

	device, err := w.Store.UpsertDevice(ctx, ev.ProjectID, dec.Identity.DeviceID, dec.Identity.Product, dec.Identity.HardwareRevision, dec.Identity.FirmwareVersion, dec.Identity.BuildID, severity)
	if err != nil {
		return err
	}
	deviceID := device.ID
	_ = w.Store.MarkDeviceHealth(ctx, deviceID, severity)
	_ = w.Store.RecordFirmwareHistory(ctx, ev.ProjectID, deviceID, dec.Identity.FirmwareVersion, dec.Identity.BuildID)
	if dec.BootID != "" || dec.Identity.BootID != "" {
		boot := dec.BootID
		if boot == "" {
			boot = dec.Identity.BootID
		}
		_ = w.Store.UpsertBootSession(ctx, ev.ProjectID, &deviceID, boot)
	}

	var releaseID *uuid.UUID
	rel, err := w.Store.UpsertRelease(ctx, ev.ProjectID, dec.Identity.FirmwareVersion, dec.Identity.BuildID, dec.Identity.GitCommit, "")
	if err != nil {
		return err
	}
	if rel.ID != uuid.Nil {
		releaseID = &rel.ID
	}

	var artifactID *uuid.UUID
	archName := lep.ArchName(dec.Header.Architecture)
	if dec.Identity.BuildID != "" {
		if art, err := w.Store.FindArtifactMatch(ctx, ev.ProjectID, dec.Identity.BuildID, archName, dec.Identity.FirmwareVersion); err == nil {
			artifactID = &art.ID
			path := w.Object.Path(art.ObjectKey)
			addrs := make([]uint64, 0, len(report.Frames))
			for _, f := range report.Frames {
				addrs = append(addrs, f.Address)
			}
			if len(addrs) == 0 && report.PC != 0 {
				addrs = append(addrs, uint64(report.PC&^1))
				if report.LR != 0 {
					addrs = append(addrs, uint64(report.LR&^1))
				}
			}
			frames, warns, err := symbolicate.Resolve(path, addrs)
			report.Warnings = append(report.Warnings, warns...)
			if err != nil {
				report.Warnings = append(report.Warnings, "symbolication: "+err.Error())
			} else if len(frames) > 0 {
				report.Frames = toAnalysisFrames(frames)
				if report.Confidence < 0.9 {
					report.Confidence += 0.15
					if report.Confidence > 1 {
						report.Confidence = 1
					}
				}
			}
		}
	}

	fp := fingerprint.Compute(dec, report)
	title := fingerprint.Title(dec, report)

	issue, err := w.Store.LinkEventToIssue(ctx, ev.ProjectID, ev.ID, fp, title, severity, report.ProbableCause, &deviceID)
	if err != nil {
		return err
	}
	issueID := issue.ID

	decodedJSON, _ := json.Marshal(dec)
	analysisJSON, _ := json.Marshal(report)
	framesJSON, _ := json.Marshal(report.Frames)

	if err := w.Store.FinalizeEvent(ctx, ev.ID, &deviceID, releaseID, artifactID, &issueID, "ready", fp, decodedJSON, analysisJSON, framesJSON); err != nil {
		return err
	}
	_ = w.Store.SetEventAnalyzerVersion(ctx, ev.ID, analysis.AnalyzerVersion, SymbolizerVersion)

	if issue.IsNew || issue.IsRegression {
		kind := "new_issue"
		if severity == "fatal" {
			kind = "new_fatal_issue"
		}
		dedupe := "new"
		if issue.IsRegression {
			dedupe = fmt.Sprintf("reg-%d", issue.RegressionCount)
		}
		_ = w.Store.EnqueueJob(ctx, "notify_issue", map[string]string{
			"project_id": ev.ProjectID.String(),
			"issue_id":   issueID.String(),
			"kind":       kind,
			"dedupe":     dedupe,
		})
	}
	return nil
}

func (w *Worker) notifyIssue(ctx context.Context, projectID, issueID uuid.UUID, kind string) error {
	issue, err := w.Store.GetIssueForWorker(ctx, projectID, issueID)
	if err != nil {
		return err
	}
	kinds := []string{kind}
	if kind == "new_fatal_issue" {
		kinds = append(kinds, "new_issue")
	}
	payload := map[string]any{
		"type":       kind,
		"issue":      issue,
		"project_id": projectID.String(),
	}

	// legacy alert_rules webhook path
	for _, k := range kinds {
		rules, err := w.Store.RulesForKind(ctx, projectID, k)
		if err != nil {
			return err
		}
		for _, rule := range rules {
			channel := rule.Channel
			if channel == "" {
				channel = "webhook"
			}
			target := rule.TargetURL
			cfg := map[string]any{}
			if rule.ChannelID != nil {
				if ck, _, secret, conf, err := w.Store.GetChannel(ctx, projectID, *rule.ChannelID); err == nil {
					channel = ck
					_ = json.Unmarshal(conf, &cfg)
					if secret != "" {
						rule.Secret = secret
					}
					if u, ok := cfg["webhook_url"].(string); ok && target == "" {
						target = u
					}
				}
			}
			if !rule.Enabled {
				rid := rule.ID
				w.Store.LogAlertSkip(ctx, projectID, &issueID, &rid, kind, "disabled", rule.Name)
				continue
			}
			attempts := rule.MaxRetries
			if attempts <= 0 {
				attempts = 4
			}
			tmpl := strCfg(cfg, "template")
			last, history := notify.DeliverWithRetry(ctx, channel, target, rule.Secret, cfg, payload, notify.Options{
				MaxAttempts: attempts,
				BaseDelay:   time.Second,
				MaxDelay:    8 * time.Second,
				Template:    tmpl,
			})
			rid := rule.ID
			_ = w.Store.RecordWebhook(ctx, projectID, &rid, kind, target, last.StatusCode, last.Success, last.Body+last.Error, payload)
			_ = w.Store.RecordNotifyDelivery(ctx, projectID, channel, target, last.Success, last.StatusCode, len(history), last.Error+last.Body)
			if last.Success {
				_ = w.Store.MarkAlertFired(ctx, rule.ID)
			} else {
				w.Store.LogAlertSkip(ctx, projectID, &issueID, &rid, kind, "error", last.Error+last.Body)
			}
		}
	}

	// also fan-out all enabled notification_channels for the kind via extra config
	channels, _ := w.Store.ListChannels(ctx, projectID)
	for _, ch := range channels {
		if en, _ := ch["enabled"].(bool); !en {
			continue
		}
		id, _ := ch["id"].(uuid.UUID)
		if id == uuid.Nil {
			if s, ok := ch["id"].(string); ok {
				id, _ = uuid.Parse(s)
			}
		}
		if id == uuid.Nil {
			continue
		}
		kindCh, _, secret, conf, err := w.Store.GetChannel(ctx, projectID, id)
		if err != nil {
			continue
		}
		var cfg map[string]any
		_ = json.Unmarshal(conf, &cfg)
		res, hist := notify.DeliverWithRetry(ctx, kindCh, strCfg(cfg, "webhook_url"), secret, cfg, payload, notify.Options{
			MaxAttempts: 4,
			BaseDelay:   500 * time.Millisecond,
			Template:    strCfg(cfg, "template"),
		})
		_ = w.Store.RecordWebhook(ctx, projectID, nil, kind, kindCh, res.StatusCode, res.Success, res.Body+res.Error, payload)
		_ = w.Store.RecordNotifyDelivery(ctx, projectID, kindCh, strCfg(cfg, "webhook_url"), res.Success, res.StatusCode, len(hist), res.Error+res.Body)
	}

	// On-call: annotate payload and page whoever is on shift
	if oncall, err := w.Store.WhoIsOnCall(ctx, projectID, time.Now().UTC()); err == nil && len(oncall) > 0 {
		payload["oncall"] = oncall
		// Prefer project email channel config for SMTP
		var smtpCfg map[string]any
		if chs, _ := w.Store.ListChannels(ctx, projectID); chs != nil {
			for _, ch := range chs {
				if k, _ := ch["kind"].(string); strings.EqualFold(k, "email") {
					if id, ok := ch["id"].(uuid.UUID); ok {
						if _, _, secret, conf, err := w.Store.GetChannel(ctx, projectID, id); err == nil {
							_ = json.Unmarshal(conf, &smtpCfg)
							if smtpCfg == nil {
								smtpCfg = map[string]any{}
							}
							if secret != "" {
								smtpCfg["password"] = secret
							}
						}
					}
				}
			}
		}
		for _, email := range oncall {
			ok := false
			if smtpCfg != nil {
				cfg := map[string]any{}
				for k, v := range smtpCfg {
					cfg[k] = v
				}
				cfg["to"] = email
				res, _ := notify.DeliverWithRetry(ctx, "email", "", "", cfg, payload, notify.Options{MaxAttempts: 2})
				ok = res.Success
			}
			w.Store.LogEscalationFire(ctx, projectID, &issueID, nil, 0, "oncall:"+email, ok)
		}
	}

	// Escalation policies: level 0 immediate webhooks from policy levels JSON
	if policies, err := w.Store.ListEscalationPolicies(ctx, projectID); err == nil {
		for _, pol := range policies {
			if !pol.Enabled {
				continue
			}
			var levels []map[string]any
			_ = json.Unmarshal(pol.Levels, &levels)
			for li, lvl := range levels {
				delay := 0
				if d, ok := lvl["delay_sec"].(float64); ok {
					delay = int(d)
				}
				if delay > 0 {
					// schedule delayed escalation step
					step := map[string]any{
						"project_id": projectID.String(),
						"issue_id":   issueID.String(),
						"policy_id":  pol.ID.String(),
						"level":      li,
						"level_cfg":  lvl,
						"payload":    payload,
					}
					_ = w.Store.EnqueueJobDelayed(ctx, "notify_escalation", step, time.Duration(delay)*time.Second)
					continue
				}
				w.fireEscalationLevel(ctx, projectID, issueID, pol.ID, li, lvl, payload)
			}
		}
	}
	return nil
}

func strCfg(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func toAnalysisFrames(in []symbolicate.Frame) []analysis.Frame {
	out := make([]analysis.Frame, len(in))
	for i, f := range in {
		out[i] = analysis.Frame{Address: f.Address, Function: f.Function, File: f.File, Line: f.Line, Inline: f.Inline}
	}
	return out
}
