package queue

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

type Job struct {
	ID      uuid.UUID
	Type    string
	Payload json.RawMessage
	Attempt int
	Lease   string
}

type Queue struct {
	Pool *pgxpool.Pool
}

func (q *Queue) Claim(ctx context.Context, leaseFor time.Duration) (Job, error) {
	tx, err := q.Pool.Begin(ctx)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback(ctx)

	var j Job
	err = tx.QueryRow(ctx, `
SELECT id, type, payload, attempts
FROM jobs
WHERE status IN ('ready','retry') AND available_at <= now()
  AND (leased_until IS NULL OR leased_until < now())
ORDER BY created_at
FOR UPDATE SKIP LOCKED
LIMIT 1`).Scan(&j.ID, &j.Type, &j.Payload, &j.Attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, pgx.ErrNoRows
	}
	if err != nil {
		return Job{}, err
	}
	j.Lease = auth.RandomHex(16)
	j.Attempt++
	_, err = tx.Exec(ctx, `
UPDATE jobs SET status='leased', attempts=$2, lease_token=$3, leased_until=$4
WHERE id=$1`, j.ID, j.Attempt, j.Lease, time.Now().Add(leaseFor))
	if err != nil {
		return Job{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, err
	}
	return j, nil
}

func (q *Queue) Complete(ctx context.Context, id uuid.UUID, lease string) error {
	_, err := q.Pool.Exec(ctx, `
UPDATE jobs SET status='done', lease_token=NULL, leased_until=NULL WHERE id=$1 AND lease_token=$2`, id, lease)
	return err
}

func (q *Queue) Fail(ctx context.Context, id uuid.UUID, lease, msg string, retryIn time.Duration, maxAttempts int) error {
	next := time.Now().Add(retryIn)
	ct, err := q.Pool.Exec(ctx, `
UPDATE jobs SET
  status = CASE WHEN attempts >= $4 THEN 'dead' ELSE 'retry' END,
  last_error=$3,
  available_at = CASE WHEN attempts >= $4 THEN available_at ELSE $5 END,
  lease_token=NULL,
  leased_until=NULL
WHERE id=$1 AND lease_token=$2`, id, lease, msg, maxAttempts, next)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("job lease mismatch")
	}
	return nil
}
