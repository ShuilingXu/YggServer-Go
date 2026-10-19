package identity

import (
	"context"
	"crypto/rand"
	"database/sql"
	"github.com/ShuilingXu/YggServer-Go/plugin"
	"math/big"
	"net/http"
	"time"

	"github.com/ShuilingXu/YggServer-Go/internal/audit"
	"github.com/ShuilingXu/YggServer-Go/internal/httpapi"
	"github.com/ShuilingXu/YggServer-Go/internal/model"
	"github.com/ShuilingXu/YggServer-Go/internal/site"
)

type PointEntry struct {
	ID        int64          `json:"id"`
	Delta     int64          `json:"delta"`
	Kind      site.PointKind `json:"kind"`
	CreatedAt time.Time      `json:"createdAt"`
}
type Wallet struct {
	Balance    int64        `json:"balance"`
	NextSignAt *time.Time   `json:"nextSignAt"`
	Entries    []PointEntry `json:"entries"`
}

func (s *Store) Wallet(ctx context.Context, id int64, hours int) (Wallet, error) {
	w := Wallet{Entries: []PointEntry{}}
	var last sql.NullTime
	err := s.DB.QueryRowContext(ctx, s.q("SELECT COALESCE(SUM(delta),0),MAX(CASE WHEN kind=1 THEN created_at END) FROM point_entries WHERE account_id=?"), id).Scan(&w.Balance, &last)
	if err != nil {
		return w, storeError(err)
	}
	if last.Valid {
		next := last.Time.UTC().Add(time.Duration(hours) * time.Hour)
		w.NextSignAt = &next
	}
	rows, err := s.DB.QueryContext(ctx, s.q("SELECT id,delta,kind,created_at FROM point_entries WHERE account_id=? ORDER BY id DESC LIMIT 50"), id)
	if err != nil {
		return w, storeError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var e PointEntry
		if err := rows.Scan(&e.ID, &e.Delta, &e.Kind, &e.CreatedAt); err != nil {
			return w, storeError(err)
		}
		w.Entries = append(w.Entries, e)
	}
	return w, storeError(rows.Err())
}

func (s *Store) Sign(ctx context.Context, id int64, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	a, err := scanAccount(tx.QueryRowContext(ctx, s.q("SELECT "+accountColumns+" FROM accounts a WHERE id=? FOR UPDATE"), id))
	if err != nil {
		return err
	}
	if a.State != model.AccountActive {
		return ErrDenied
	}
	c, err := site.Read(ctx, tx)
	if err != nil {
		return err
	}
	if c.SignEnabled != 1 {
		return ErrDenied
	}
	var last sql.NullTime
	if err := tx.QueryRowContext(ctx, s.q("SELECT MAX(created_at) FROM point_entries WHERE account_id=? AND kind=1"), id).Scan(&last); err != nil {
		return storeError(err)
	}
	if last.Valid && now.Before(last.Time.Add(time.Duration(c.SignHours)*time.Hour)) {
		return site.ErrCooldown
	}
	event := plugin.Event{Action: plugin.CheckIn, AccountID: a.ID, AccountUUID: a.UUID, At: now.UTC()}
	if err := s.Plugins.Before(ctx, event); err != nil {
		return err
	}
	n, err := rand.Int(rand.Reader, big.NewInt(c.SignMax-c.SignMin+1))
	if err != nil {
		return err
	}
	// 零奖励也记录签到，以免关闭奖励后可无限重复提交。
	_, err = tx.ExecContext(ctx, s.q("INSERT INTO point_entries(account_id,delta,kind,created_at) VALUES(?,?,?,?)"), id, c.SignMin+n.Int64(), site.PointSign, now.UTC())
	if err != nil {
		return storeError(err)
	}
	if err := tx.Commit(); err != nil {
		return storeError(err)
	}
	s.Plugins.After(ctx, event)
	return nil
}

func (s *Store) SaveSettings(ctx context.Context, actor Account, c site.Settings, requestID string, now time.Time) error {
	if err := c.Validate(); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	a, err := scanAccount(tx.QueryRowContext(ctx, s.q("SELECT "+accountColumns+" FROM accounts a WHERE id=? FOR UPDATE"), actor.ID))
	if err != nil {
		return err
	}
	if a.State != model.AccountActive || a.Permission != model.PermissionOwner {
		return ErrDenied
	}
	_, err = tx.ExecContext(ctx, s.q("UPDATE site_settings SET name=?,description=?,announcement=?,registration=?,email_mode=?,email_domains=?,max_players=?,single_session=?,sign_enabled=?,sign_hours=?,sign_min=?,sign_max=?,initial_points=?,player_cost=?,oauth_enabled=?,upload_cost=?,private_upload_cost=?,closet_cost=?,skin_domains=?,oidc_enabled=?,oidc_issuer=?,oidc_client_id=?,oidc_name=? WHERE id=1"), c.Name, c.Description, c.Announcement, c.Registration, c.EmailMode, c.EmailDomains, c.MaxPlayers, c.SingleSession, c.SignEnabled, c.SignHours, c.SignMin, c.SignMax, c.InitialPoints, c.PlayerCost, c.OAuthEnabled, c.UploadCost, c.PrivateUploadCost, c.ClosetCost, c.SkinDomains, c.OIDCEnabled, c.OIDCIssuer, c.OIDCClientID, c.OIDCName)
	if err != nil {
		return storeError(err)
	}
	if err := audit.Record(ctx, tx, s.Dialect, a.UUID, "", model.AuditSiteSettings, requestID, now, model.AuditApplied, -1, -1); err != nil {
		return storeError(err)
	}
	return storeError(tx.Commit())
}

func (h *Handler) registerSite(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/site", func(w http.ResponseWriter, r *http.Request) {
		if h.Service == nil {
			h.failure(w, r, ErrUnavailable)
			return
		}
		c, err := site.Read(r.Context(), h.Service.Store.DB)
		if err != nil {
			h.failure(w, r, err)
			return
		}
		httpapi.JSON(w, 200, c)
	})
	mux.HandleFunc("PUT /api/admin/site", h.Authorized(model.TokenWeb, func(w http.ResponseWriter, r *http.Request) {
		var c site.Settings
		if !httpapi.Decode(w, r, &c) {
			return
		}
		a := Current(r.Context()).Account
		if err := h.Service.Store.SaveSettings(r.Context(), a, c, w.Header().Get("X-Request-ID"), h.Service.Now()); err != nil {
			h.failure(w, r, err)
			return
		}
		h.Logger.InfoContext(r.Context(), "site settings updated", "account_id", a.ID, "request_id", w.Header().Get("X-Request-ID"))
		w.WriteHeader(204)
	}))
	for _, route := range []string{"GET /api/account/points", "POST /api/account/sign"} {
		mux.HandleFunc(route, h.Authorized(model.TokenWeb, func(w http.ResponseWriter, r *http.Request) {
			a := Current(r.Context()).Account
			if r.Method == "POST" {
				if err := h.Service.Store.Sign(r.Context(), a.ID, h.Service.Now()); err != nil {
					h.failure(w, r, err)
					return
				}
				h.Logger.InfoContext(r.Context(), "check-in completed", "account_id", a.ID, "request_id", w.Header().Get("X-Request-ID"))
			}
			c, err := site.Read(r.Context(), h.Service.Store.DB)
			if err != nil {
				h.failure(w, r, err)
				return
			}
			v, err := h.Service.Store.Wallet(r.Context(), a.ID, c.SignHours)
			if err != nil {
				h.failure(w, r, err)
				return
			}
			httpapi.JSON(w, 200, v)
		}))
	}
	h.registerOAuth(mux)
}
