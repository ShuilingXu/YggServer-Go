package skin

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/ShuilingXu/YggServer-Go/internal/audit"
	"github.com/ShuilingXu/YggServer-Go/internal/model"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

type Report struct {
	UUID         string            `json:"id"`
	TextureUUID  string            `json:"texture"`
	ReporterUUID string            `json:"reporter"`
	Reason       string            `json:"reason"`
	State        model.ReportState `json:"state"`
	CreatedAt    time.Time         `json:"createdAt"`
	HandledAt    *time.Time        `json:"handledAt"`
	HandlerUUID  string            `json:"handler"`
	Note         string            `json:"note"`
}

func (s *Store) Report(ctx context.Context, accountID int64, uuid, reason string, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return storageError(err)
	}
	defer tx.Rollback()
	if err := s.lockAccount(ctx, tx, accountID); err != nil {
		return err
	}
	t, err := scan(tx.QueryRowContext(ctx, s.q("SELECT "+columns+" FROM textures t JOIN accounts a ON a.id=t.account_id WHERE t.uuid=? AND a.state=0 AND t.state=0 AND (t.is_public=1 OR t.account_id=?) FOR UPDATE"), uuid, accountID))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, s.q("INSERT INTO reports(uuid,texture_id,reporter_id,reason,created_at) VALUES(?,?,?,?,?)"), model.UUID(), t.ID, accountID, reason, now)
	var pg *pgconn.PgError
	var my *mysql.MySQLError
	if (errors.As(err, &pg) && pg.Code == "23505") || (errors.As(err, &my) && my.Number == 1062) {
		return ErrConflict
	}
	if err != nil {
		return storageError(err)
	}
	return storageError(tx.Commit())
}

func (s *Store) Reports(ctx context.Context, offset int) ([]Report, error) {
	rows, err := s.DB.QueryContext(ctx, s.q("SELECT r.uuid,t.uuid,a.uuid,r.reason,r.state,r.created_at,r.handled_at,COALESCE(h.uuid,''),r.note FROM reports r JOIN textures t ON t.id=r.texture_id JOIN accounts a ON a.id=r.reporter_id LEFT JOIN accounts h ON h.id=r.handler_id ORDER BY r.id DESC LIMIT 50 OFFSET ?"), offset)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	reports := []Report{}
	for rows.Next() {
		var r Report
		if err := rows.Scan(&r.UUID, &r.TextureUUID, &r.ReporterUUID, &r.Reason, &r.State, &r.CreatedAt, &r.HandledAt, &r.HandlerUUID, &r.Note); err != nil {
			return nil, storageError(err)
		}
		reports = append(reports, r)
	}
	return reports, storageError(rows.Err())
}

func (s *Store) Moderate(ctx context.Context, actorID int64, uuid string, state model.TextureState, report bool, reportState model.ReportState, note, requestID string, now time.Time) error {
	if state < model.TextureActive || state > model.TextureHidden || utf8.RuneCountInString(note) > 1000 {
		return ErrInvalid
	}
	if report && (reportState != model.ReportResolved && reportState != model.ReportRejected) {
		return ErrInvalid
	}
	var uploaderID int64
	query := "SELECT account_id FROM textures WHERE uuid=?"
	if report {
		query = "SELECT t.account_id FROM textures t JOIN reports r ON r.texture_id=t.id WHERE r.uuid=?"
	}
	if err := s.DB.QueryRowContext(ctx, s.q(query), uuid).Scan(&uploaderID); err != nil {
		return storageError(err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return storageError(err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, s.q("SELECT id,uuid,state,permission FROM accounts WHERE id=? OR id=? ORDER BY id FOR UPDATE"), actorID, uploaderID)
	if err != nil {
		return storageError(err)
	}
	var actorUUID string
	var actorPermission, uploaderPermission model.Permission
	for rows.Next() {
		var id int64
		var accountUUID string
		var accountState model.AccountState
		var permission model.Permission
		if err := rows.Scan(&id, &accountUUID, &accountState, &permission); err != nil {
			rows.Close()
			return storageError(err)
		}
		if id == actorID && accountState == model.AccountActive {
			actorUUID = accountUUID
			actorPermission = permission
		}
		if id == uploaderID {
			uploaderPermission = permission
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return storageError(err)
	}
	if actorUUID == "" || actorPermission < model.PermissionAdmin || (actorPermission == model.PermissionAdmin && uploaderPermission != model.PermissionUser) {
		return ErrDenied
	}
	target := uuid
	action := model.AuditTextureModerate
	var textureID int64
	if report {
		var id int64
		var current model.ReportState
		err = tx.QueryRowContext(ctx, s.q("SELECT r.id,r.state,r.texture_id,t.uuid FROM reports r JOIN textures t ON t.id=r.texture_id WHERE r.uuid=? FOR UPDATE"), uuid).Scan(&id, &current, &textureID, &target)
		if err != nil {
			return storageError(err)
		}
		if current != model.ReportPending {
			return ErrConflict
		}
		// 两种数据库共用唯一约束：0 代表待处理，处理后用举报主键解除占位。
		_, err = tx.ExecContext(ctx, s.q("UPDATE reports SET state=?,dedup_key=id,handled_at=?,handler_id=?,note=? WHERE id=?"), reportState, now, actorID, note, id)
		if err != nil {
			return storageError(err)
		}
		action = model.AuditReportResolve
	} else {
		err = tx.QueryRowContext(ctx, s.q("SELECT id FROM textures WHERE uuid=? FOR UPDATE"), uuid).Scan(&textureID)
		if err != nil {
			return storageError(err)
		}
	}
	if !report || reportState == model.ReportResolved {
		_, err = tx.ExecContext(ctx, s.q("UPDATE textures SET state=? WHERE id=?"), state, textureID)
		if err != nil {
			return storageError(err)
		}
	}
	result := model.AuditApplied
	targetState := int16(state)
	if report && reportState == model.ReportRejected {
		result = model.AuditDismissed
		targetState = model.AuditUnchanged
	}
	if err := audit.Record(ctx, tx, s.Dialect, actorUUID, target, action, requestID, now, result, targetState, model.AuditUnchanged); err != nil {
		return storageError(err)
	}
	return storageError(tx.Commit())
}
