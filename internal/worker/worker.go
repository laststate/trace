package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/laststate/trace/internal/analysis"
	"github.com/laststate/trace/internal/decode"
	"github.com/laststate/trace/internal/fingerprint"
	"github.com/laststate/trace/internal/lep"
	"github.com/laststate/trace/internal/metrics"
	"github.com/laststate/trace/internal/objects"
	"github.com/laststate/trace/internal/queue"
	"github.com/laststate/trace/internal/store"
	"github.com/laststate/trace/internal/symbolicate"
	"github.com/laststate/trace/internal/webhook"
)

type Worker struct {
	Store  *store.Store
	Queue  *queue.Queue
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

func (w *Worker) tick(ctx context.Context) {
	job, err := w.Queue.Claim(ctx, w.Lease)
	if errors.Is(err, pgx.ErrNoRows) {
		return
	}
	if err != nil {
		w.Log.Error("claim job", "err", err)
		return
	}
	if err := w.handle(ctx, job); err != nil {
		metrics.JobsFailed.Add(1)
		w.Log.Error("job failed", "id", job.ID, "type", job.Type, "err", err)
		_ = w.Queue.Fail(ctx, job.ID, job.Lease, err.Error(), time.Duration(job.Attempt)*time.Second, 20)
		return
	}
	metrics.JobsCompleted.Add(1)
	_ = w.Queue.Complete(ctx, job.ID, job.Lease)
}

func (w *Worker) handle(ctx context.Context, job queue.Job) error {
	switch job.Type {
	case "process_event":
		var p struct {
			EventID string `json:"event_id"`
		}
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return err
		}
		id, err := uuid.Parse(p.EventID)
		if err != nil {
			return err
		}
		return w.processEvent(ctx, id)
	case "notify_issue":
		var p struct {
			ProjectID string `json:"project_id"`
			IssueID   string `json:"issue_id"`
			Kind      string `json:"kind"`
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
		return w.notifyIssue(ctx, pid, iid, p.Kind)
	default:
		return errors.New("unknown job type: " + job.Type)
	}
}

func (w *Worker) processEvent(ctx context.Context, id uuid.UUID) error {
	ev, err := w.Store.GetEventByID(ctx, id)
	if err != nil {
		return err
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
	report := analysis.FromDecoded(dec)

	device, err := w.Store.UpsertDevice(ctx, ev.ProjectID, dec.Identity.DeviceID, dec.Identity.Product, dec.Identity.HardwareRevision, dec.Identity.FirmwareVersion, dec.Identity.BuildID)
	if err != nil {
		return err
	}
	deviceID := device.ID

	var releaseID *uuid.UUID
	rel, err := w.Store.UpsertRelease(ctx, ev.ProjectID, dec.Identity.FirmwareVersion, dec.Identity.BuildID)
	if err != nil {
		return err
	}
	if rel.ID != uuid.Nil {
		releaseID = &rel.ID
	}

	var artifactID *uuid.UUID
	if dec.Identity.BuildID != "" {
		if art, err := w.Store.FindArtifactByBuildID(ctx, ev.ProjectID, dec.Identity.BuildID); err == nil {
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

	severity := "error"
	if dec.Event != nil {
		severity = decode.SeverityName(dec.Event.Severity)
	} else if dec.Header.Type == lep.TypeCrash || dec.Header.Type == lep.TypeCoredump {
		severity = "fatal"
	}

	issue, err := w.Store.UpsertIssue(ctx, ev.ProjectID, fp, title, severity, report.ProbableCause, &deviceID)
	if err != nil {
		return err
	}
	issueID := issue.ID
	created := issue.EventCount == 1

	decodedJSON, _ := json.Marshal(dec)
	analysisJSON, _ := json.Marshal(report)
	framesJSON, _ := json.Marshal(report.Frames)

	if err := w.Store.FinalizeEvent(ctx, id, &deviceID, releaseID, artifactID, &issueID, "ready", fp, decodedJSON, analysisJSON, framesJSON); err != nil {
		return err
	}

	if created {
		kind := "new_issue"
		if severity == "fatal" {
			kind = "new_fatal_issue"
		}
		_ = w.Store.EnqueueJob(ctx, "notify_issue", map[string]string{
			"project_id": ev.ProjectID.String(),
			"issue_id":   issueID.String(),
			"kind":       kind,
		})
	}
	return nil
}

func (w *Worker) notifyIssue(ctx context.Context, projectID, issueID uuid.UUID, kind string) error {
	issue, err := w.Store.GetIssue(ctx, issueID)
	if err != nil {
		return err
	}
	// also fire new_issue rules for fatal
	kinds := []string{kind}
	if kind == "new_fatal_issue" {
		kinds = append(kinds, "new_issue")
	}
	payload := map[string]any{
		"type":       kind,
		"issue":      issue,
		"project_id": projectID.String(),
	}
	for _, k := range kinds {
		rules, err := w.Store.RulesForKind(ctx, projectID, k)
		if err != nil {
			return err
		}
		for _, rule := range rules {
			if rule.Channel != "webhook" || rule.TargetURL == "" {
				continue
			}
			d, err := webhook.Send(ctx, rule.TargetURL, rule.Secret, payload)
			rid := rule.ID
			body := ""
			code := 0
			ok := false
			if err != nil {
				body = err.Error()
			} else {
				body, code, ok = d.Body, d.StatusCode, d.Success
			}
			_ = w.Store.RecordWebhook(ctx, projectID, &rid, kind, rule.TargetURL, code, ok, body, payload)
		}
	}
	return nil
}

func toAnalysisFrames(in []symbolicate.Frame) []analysis.Frame {
	out := make([]analysis.Frame, len(in))
	for i, f := range in {
		out[i] = analysis.Frame{Address: f.Address, Function: f.Function, File: f.File, Line: f.Line, Inline: f.Inline}
	}
	return out
}
