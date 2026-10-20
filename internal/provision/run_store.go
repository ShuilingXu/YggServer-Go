package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ShuilingXu/YggServer-Go/internal/database"
)

func (s *Store) recoverRuns(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE import_runs SET state=2,completed_at=CURRENT_TIMESTAMP WHERE state=0")
	if err != nil {
		return fmt.Errorf("recover import runs: %w", err)
	}
	return nil
}
func (s *Store) recordRun(ctx context.Context, run Run, create bool) error {
	raw, err := json.Marshal(run.Report)
	if err != nil {
		return fmt.Errorf("encode import report: %w", err)
	}
	if create {
		_, err = s.DB.ExecContext(ctx, database.Bind(s.Dialect, "INSERT INTO import_runs(uuid,dry_run,state,report,created_at) VALUES(?,?,0,?,?)"), run.ID, boolInt(run.DryRun), string(raw), run.CreatedAt)
	} else {
		_, err = s.DB.ExecContext(ctx, database.Bind(s.Dialect, "UPDATE import_runs SET state=?,report=?,completed_at=? WHERE uuid=?"), run.State, string(raw), run.CompletedAt, run.ID)
	}
	if err != nil {
		return fmt.Errorf("record import run: %w", err)
	}
	return nil
}
func (s *Store) history(ctx context.Context) ([]Run, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT uuid,dry_run,state,report,created_at,completed_at FROM import_runs ORDER BY id DESC LIMIT 20")
	if err != nil {
		return nil, fmt.Errorf("read import runs: %w", err)
	}
	defer rows.Close()
	runs := []Run{}
	for rows.Next() {
		var run Run
		var dry int
		var raw string
		if err = rows.Scan(&run.ID, &dry, &run.State, &raw, &run.CreatedAt, &run.CompletedAt); err != nil {
			return nil, fmt.Errorf("read import run: %w", err)
		}
		run.DryRun = dry == 1
		run.Stage = "failed"
		if run.State == RunSuccess {
			run.Stage = "complete"
		}
		if run.State == RunCanceled {
			run.Stage = "canceled"
		}
		if run.State == RunRunning {
			run.Stage = "queued"
		}
		if err = json.Unmarshal([]byte(raw), &run.Report); err != nil {
			return nil, fmt.Errorf("decode import report: %w", err)
		}
		runs = append(runs, run)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read import runs: %w", err)
	}
	return runs, nil
}
