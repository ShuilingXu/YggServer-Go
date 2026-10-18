package audit

import (
	"context"
	"database/sql"
	"time"

	"github.com/ShuilingXu/YggServer-Go/internal/database"
	"github.com/ShuilingXu/YggServer-Go/internal/model"
)

type Event struct {
	ID               int64             `json:"id"`
	Actor            string            `json:"actor"`
	Target           string            `json:"target"`
	Action           model.AuditAction `json:"action"`
	RequestID        string            `json:"requestId"`
	Result           model.AuditResult `json:"result"`
	TargetState      int16             `json:"targetState"`
	TargetPermission int16             `json:"targetPermission"`
	CreatedAt        time.Time         `json:"createdAt"`
}

func Record(ctx context.Context, tx *sql.Tx, dialect, actor, target string, action model.AuditAction, requestID string, now time.Time, result model.AuditResult, state, permission int16) error {
	_, err := tx.ExecContext(ctx, database.Bind(dialect, "INSERT INTO audit_events(actor_uuid,target_uuid,action,request_id,created_at,result,target_state,target_permission) VALUES(?,?,?,?,?,?,?,?)"), actor, target, action, requestID, now.UTC(), result, state, permission)
	return err
}

func List(ctx context.Context, db *sql.DB, dialect string, offset int) ([]Event, error) {
	rows, err := db.QueryContext(ctx, database.Bind(dialect, "SELECT id,actor_uuid,target_uuid,action,request_id,created_at,result,target_state,target_permission FROM audit_events ORDER BY id DESC LIMIT 50 OFFSET ?"), offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Actor, &e.Target, &e.Action, &e.RequestID, &e.CreatedAt, &e.Result, &e.TargetState, &e.TargetPermission); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
