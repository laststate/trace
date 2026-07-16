// Package analytics provides a lightweight “warehouse-shaped” export path
// (NDJSON batches) so fleets can pipe into ClickHouse/BigQuery without
// requiring those systems inside Trace.
package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/objects"
	"github.com/laststate/trace/internal/store"
)

type Sink interface {
	WriteNDJSON(ctx context.Context, projectID uuid.UUID, rows []map[string]any) (objectKey string, n int, err error)
}

// FileSink writes under ObjectDir/analytics/
type FileSink struct {
	Root string
}

func (f FileSink) WriteNDJSON(ctx context.Context, projectID uuid.UUID, rows []map[string]any) (string, int, error) {
	dir := filepath.Join(f.Root, "analytics", projectID.String())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}
	name := fmt.Sprintf("events-%s.ndjson", time.Now().UTC().Format("20060102T150405"))
	path := filepath.Join(dir, name)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return "", 0, err
		}
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return "", 0, err
	}
	key := filepath.ToSlash(filepath.Join("analytics", projectID.String(), name))
	return key, len(rows), nil
}

// ObjectSink stores NDJSON via objects.Store (content-addressed Put).
type ObjectSink struct {
	Objects *objects.Store
}

func (o ObjectSink) WriteNDJSON(ctx context.Context, projectID uuid.UUID, rows []map[string]any) (string, int, error) {
	if o.Objects == nil {
		return "", 0, fmt.Errorf("no object store")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, r := range rows {
		_ = enc.Encode(r)
	}
	key, _, err := o.Objects.Put(buf.Bytes())
	if err != nil {
		return "", 0, err
	}
	return key, len(rows), nil
}

// ExportRecent dumps project events since `since` through sink and logs the export.
func ExportRecent(ctx context.Context, st *store.Store, sink Sink, projectID uuid.UUID, since time.Time, limit int) (string, int, error) {
	rows, err := st.ExportEventsNDJSON(ctx, projectID, since, limit)
	if err != nil {
		return "", 0, err
	}
	if len(rows) == 0 {
		return "", 0, nil
	}
	key, n, err := sink.WriteNDJSON(ctx, projectID, rows)
	if err != nil {
		return "", 0, err
	}
	_ = st.LogAnalyticsExport(ctx, projectID, "events_ndjson", key, n)
	return key, n, nil
}
