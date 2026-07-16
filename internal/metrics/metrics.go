package metrics

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

// Process-local counters (Prometheus text format, no extra deps).
var (
	IngestTotal     atomic.Int64
	IngestAccepted  atomic.Int64
	IngestDuplicate atomic.Int64
	IngestRejected  atomic.Int64
	JobsCompleted   atomic.Int64
	JobsFailed      atomic.Int64
	ArtifactUploads atomic.Int64
)

func Handler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = fmt.Fprintf(w, "# HELP trace_ingest_total Ingest requests\n# TYPE trace_ingest_total counter\ntrace_ingest_total %d\n", IngestTotal.Load())
	_, _ = fmt.Fprintf(w, "# HELP trace_ingest_accepted_total Accepted events\n# TYPE trace_ingest_accepted_total counter\ntrace_ingest_accepted_total %d\n", IngestAccepted.Load())
	_, _ = fmt.Fprintf(w, "# HELP trace_ingest_duplicate_total Duplicate events\n# TYPE trace_ingest_duplicate_total counter\ntrace_ingest_duplicate_total %d\n", IngestDuplicate.Load())
	_, _ = fmt.Fprintf(w, "# HELP trace_ingest_rejected_total Rejected events\n# TYPE trace_ingest_rejected_total counter\ntrace_ingest_rejected_total %d\n", IngestRejected.Load())
	_, _ = fmt.Fprintf(w, "# HELP trace_jobs_completed_total Completed jobs\n# TYPE trace_jobs_completed_total counter\ntrace_jobs_completed_total %d\n", JobsCompleted.Load())
	_, _ = fmt.Fprintf(w, "# HELP trace_jobs_failed_total Failed jobs\n# TYPE trace_jobs_failed_total counter\ntrace_jobs_failed_total %d\n", JobsFailed.Load())
	_, _ = fmt.Fprintf(w, "# HELP trace_artifact_uploads_total Artifact uploads\n# TYPE trace_artifact_uploads_total counter\ntrace_artifact_uploads_total %d\n", ArtifactUploads.Load())
}
