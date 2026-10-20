package provision

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/ShuilingXu/YggServer-Go/internal/audit"
	"github.com/ShuilingXu/YggServer-Go/internal/database"
	"github.com/ShuilingXu/YggServer-Go/internal/identity"
	"github.com/ShuilingXu/YggServer-Go/internal/model"
	"github.com/ShuilingXu/YggServer-Go/internal/site"
	"github.com/ShuilingXu/YggServer-Go/internal/skin"
)

type Source struct {
	Driver         string            `json:"driver"`
	DSN            string            `json:"dsn"`
	Prefix         string            `json:"prefix"`
	TexturesDir    string            `json:"texturesDir"`
	TimeZone       string            `json:"timeZone"`
	PasswordMethod string            `json:"passwordMethod"`
	UUIDOverrides  map[string]string `json:"uuidOverrides"`
	ImportSettings bool              `json:"importSettings"`
}
type Issue struct {
	Table string `json:"table"`
	ID    string `json:"id"`
	Code  string `json:"code"`
}
type Report struct {
	Counts   map[string]int  `json:"counts"`
	Skipped  map[string]int  `json:"skipped"`
	Issues   []Issue         `json:"issues"`
	Blocking int             `json:"blocking"`
	Players  []PlayerMapping `json:"players"`
	Omitted  []string        `json:"omitted"`
}
type PlayerMapping struct {
	PID  string `json:"pid"`
	Name string `json:"name"`
	UUID string `json:"uuid"`
}
type Store struct {
	DB                               *sql.DB
	Dialect, TexturesDir, LegacySalt string
	Now                              func() time.Time
}
type row map[string]string

var prefixPattern = regexp.MustCompile(`^[A-Za-z0-9_]{0,48}$`)
var uuidPattern = regexp.MustCompile(`^(?:[a-fA-F0-9]{32}|[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12})$`)
var errBlocked = errors.New("import blocked")

func (r *Report) issue(table, id, code string, blocking bool) {
	if len(r.Issues) < 1000 {
		r.Issues = append(r.Issues, Issue{table, id, code})
	}
	r.Skipped[table+":"+code]++
	if blocking {
		r.Blocking++
	}
}
func readRows(ctx context.Context, tx *sql.Tx, dialect, name, columns string) ([]row, error) {
	quote := "\""
	if dialect == "mysql" {
		quote = "`"
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+columns+" FROM "+quote+name+quote+" LIMIT 100001")
	if err != nil {
		return nil, fmt.Errorf("source schema unavailable")
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := []row{}
	for rows.Next() {
		if len(result) >= 100000 {
			return nil, fmt.Errorf("source table exceeds 100000 rows")
		}
		values := make([]any, len(names))
		targets := make([]any, len(names))
		for i := range values {
			targets[i] = &values[i]
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, fmt.Errorf("source read failed")
		}
		item := row{}
		for i, name := range names {
			switch v := values[i].(type) {
			case nil:
				item[name] = ""
			case []byte:
				item[name] = string(v)
			case time.Time:
				item[name] = v.Format("2006-01-02 15:04:05.999999")
			case bool:
				if v {
					item[name] = "1"
				} else {
					item[name] = "0"
				}
			default:
				item[name] = fmt.Sprint(v)
			}
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func integer(value string) int64 {
	if value == "true" {
		return 1
	}
	if value == "false" {
		return 0
	}
	n, _ := strconv.ParseInt(value, 10, 64)
	return n
}
func oldTime(value string, zone *time.Location, fallback time.Time) time.Time {
	if value != "" {
		for _, layout := range []string{"2006-01-02 15:04:05.999999999", time.RFC3339Nano} {
			if t, err := time.ParseInLocation(layout, value, zone); err == nil {
				return t.UTC()
			}
		}
	}
	return fallback
}
func sourceFile(dir, hash string) ([]byte, int, int, error) {
	if !skin.ValidHash(hash) {
		return nil, 0, 0, errBlocked
	}
	path := filepath.Join(dir, hash)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return nil, 0, 0, errBlocked
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, errBlocked
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 16<<20+1))
	if err != nil || len(data) > 16<<20 {
		return nil, 0, 0, errBlocked
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != hash {
		return nil, 0, 0, errBlocked
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > 16*1024*1024 {
		return nil, 0, 0, errBlocked
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		return nil, 0, 0, errBlocked
	}
	return data, cfg.Width, cfg.Height, nil
}

func (e *Store) Run(ctx context.Context, input Source, dry bool, actor, requestID string, advance func(string, int)) (report Report, runErr error) {
	report = Report{Counts: map[string]int{}, Skipped: map[string]int{}, Issues: []Issue{}, Players: []PlayerMapping{}, Omitted: []string{"sessions", "recovery tokens", "oauth clients and grants", "PHP plugins", "IP addresses", "avatars and preferences"}}
	if !prefixPattern.MatchString(input.Prefix) || (input.Driver != "postgres" && input.Driver != "mysql") || !filepath.IsAbs(input.TexturesDir) || e.TexturesDir == "" {
		return report, identity.ErrInvalid
	}
	zone, err := time.LoadLocation(input.TimeZone)
	if err != nil {
		return report, identity.ErrInvalid
	}
	if strings.HasPrefix(input.PasswordMethod, "SALTED2") && e.LegacySalt == "" {
		return report, fmt.Errorf("legacy salt environment is required")
	}
	source, err := database.Open(ctx, input.Driver, input.DSN)
	if err != nil {
		return report, fmt.Errorf("source connection failed")
	}
	defer source.Close()
	src, err := source.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return report, fmt.Errorf("source snapshot failed")
	}
	defer src.Rollback()
	tables := map[string][]row{}
	for _, spec := range []struct{ name, columns string }{{"users", "uid,email,password,score,permission,verified,register_at,last_sign_at"}, {"textures", "tid,name,type,hash,uploader,public,upload_at"}, {"players", "pid,uid,name,tid_skin,tid_cape,last_modified"}, {"user_closet", "user_uid,texture_tid,item_name"}, {"options", "option_name,option_value"}} {
		advance("read_"+spec.name, 0)
		data, err := readRows(ctx, src, input.Driver, input.Prefix+spec.name, spec.columns)
		if err != nil {
			return report, err
		}
		tables[spec.name] = data
	}
	// 可选插件和举报表先探测，避免 PostgreSQL 事务因缺表而进入失败状态。
	available := map[string]bool{}
	schemaRows, err := src.QueryContext(ctx, "SELECT table_name FROM information_schema.tables WHERE table_schema="+func() string {
		if input.Driver == "mysql" {
			return "DATABASE()"
		}
		return "'public'"
	}())
	if err != nil {
		return report, fmt.Errorf("source inspection failed")
	}
	for schemaRows.Next() {
		var name string
		if err = schemaRows.Scan(&name); err != nil {
			schemaRows.Close()
			return report, err
		}
		available[name] = true
	}
	err = schemaRows.Err()
	schemaRows.Close()
	if err != nil {
		return report, err
	}
	for _, name := range []string{"uuid", "reports"} {
		if available[input.Prefix+name] {
			tables[name], err = readRows(ctx, src, input.Driver, input.Prefix+name, "*")
			if err != nil {
				return report, err
			}
		}
	}
	for name := range available {
		known := false
		for _, n := range []string{"users", "textures", "players", "user_closet", "options", "uuid", "reports"} {
			if name == input.Prefix+n {
				known = true
			}
		}
		if !known && strings.HasPrefix(name, input.Prefix) {
			report.Omitted = append(report.Omitted, name)
		}
	}
	tx, err := e.DB.BeginTx(ctx, nil)
	if err != nil {
		return report, fmt.Errorf("target transaction failed")
	}
	defer tx.Rollback()
	var lock int
	if err = tx.QueryRowContext(ctx, "SELECT id FROM site_settings WHERE id=1 FOR UPDATE").Scan(&lock); err != nil {
		return report, err
	}
	q := func(query string) string { return database.Bind(e.Dialect, query) }
	now := e.Now().UTC()
	existingEmails := map[string]bool{}
	existingPlayers := map[string]bool{}
	existingUUIDs := map[string]bool{}
	for _, spec := range []struct {
		query string
		dst   map[string]bool
	}{{"SELECT email FROM accounts", existingEmails}, {"SELECT LOWER(name) FROM players", existingPlayers}, {"SELECT uuid FROM players", existingUUIDs}} {
		rows, err := tx.QueryContext(ctx, spec.query)
		if err != nil {
			return report, err
		}
		for rows.Next() {
			var value string
			if err = rows.Scan(&value); err != nil {
				rows.Close()
				return report, err
			}
			spec.dst[value] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return report, err
		}
	}
	initiallyEmpty := len(existingEmails) == 0
	if actor != "" {
		var state, permission int
		if err = tx.QueryRowContext(ctx, q("SELECT state,permission FROM accounts WHERE uuid=? FOR UPDATE"), actor).Scan(&state, &permission); err != nil || state != 0 || permission != 2 {
			return report, identity.ErrDenied
		}
	}
	randomUUIDs := false
	for _, option := range tables["options"] {
		if option["option_name"] == "ygg_uuid_algorithm" && option["option_value"] == "v4" {
			randomUUIDs = true
		}
	}
	users := map[string]int64{}
	textures := map[string]int64{}
	textureKinds := map[string]int16{}
	textureOwners := map[string]string{}
	texturePublic := map[string]bool{}
	ownerCount := 0
	var firstOwner string
	// 文件先在目标目录外的临时目录准备；失败时不删除可能已被其他请求引用的最终 hash 文件。
	stage, err := os.MkdirTemp("", "ygg-bs-stage-")
	if err != nil {
		return report, fmt.Errorf("stage unavailable")
	}
	defer os.RemoveAll(stage)
	fileHashes := map[string]bool{}
	insert := func(query string, args ...any) (int64, error) {
		if _, err := tx.ExecContext(ctx, q(query), args...); err != nil {
			return 0, fmt.Errorf("target write failed")
		}
		return 0, nil
	}
	for i, u := range tables["users"] {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		advance("users", i)
		email, emailErr := identity.Email(u["email"])
		hash, hashErr := identity.ImportPassword(u["password"], input.PasswordMethod)
		if emailErr != nil || hashErr != nil || integer(u["uid"]) <= 0 || integer(u["score"]) < 0 {
			report.issue("users", u["uid"], "invalid_user_or_password", true)
			continue
		}
		if existingEmails[email] || users[u["uid"]] != 0 {
			report.issue("users", u["uid"], "duplicate_email_or_id", true)
			continue
		}
		existingEmails[email] = true
		permission := integer(u["permission"])
		state := int16(0)
		if permission == -1 {
			state = 1
			permission = 0
		}
		if permission < 0 || permission > 2 {
			permission = 0
			report.issue("users", u["uid"], "unknown_permission", false)
		}
		uuid := model.UUID()
		verified := int16(0)
		if u["verified"] == "1" {
			verified = 1
		}
		if _, err = insert("INSERT INTO accounts(uuid,email,password_hash,state,permission,verified,created_at) VALUES(?,?,?,?,?,?,?)", uuid, email, hash, state, permission, verified, oldTime(u["register_at"], zone, now)); err != nil {
			return report, err
		}
		var id int64
		if err = tx.QueryRowContext(ctx, q("SELECT id FROM accounts WHERE uuid=?"), uuid).Scan(&id); err != nil {
			return report, err
		}
		users[u["uid"]] = id
		if permission == 2 && state == 0 {
			ownerCount++
			firstOwner = uuid
		}
		if err = site.Change(ctx, tx, e.Dialect, id, integer(u["score"]), site.PointImport, now); err != nil {
			return report, err
		}
		if u["last_sign_at"] != "" {
			if _, err = insert("INSERT INTO point_entries(account_id,delta,kind,created_at) VALUES(?,0,?,?)", id, site.PointSign, oldTime(u["last_sign_at"], zone, now)); err != nil {
				return report, err
			}
		}
		report.Counts["users"]++
	}
	if initiallyEmpty && ownerCount == 0 {
		report.issue("users", "", "owner_missing", true)
	}
	for i, t := range tables["textures"] {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		advance("textures", i)
		owner := users[t["uploader"]]
		if owner == 0 {
			report.issue("textures", t["tid"], "owner_missing", false)
			continue
		}
		kind := int16(-1)
		switch t["type"] {
		case "steve":
			kind = 0
		case "alex":
			kind = 1
		case "cape":
			kind = 2
		}
		if kind < 0 || len(t["name"]) == 0 || len(t["name"]) > 50 || (t["public"] != "0" && t["public"] != "1") {
			report.issue("textures", t["tid"], "invalid_texture", false)
			continue
		}
		data, w, h, err := sourceFile(input.TexturesDir, t["hash"])
		if err != nil {
			report.issue("textures", t["tid"], "file_missing_or_invalid", false)
			continue
		}
		if !skin.ValidDimensions(model.TextureType(kind), w, h) {
			report.issue("textures", t["tid"], "unsupported_dimensions", false)
			continue
		}
		if !fileHashes[t["hash"]] {
			if err = os.WriteFile(filepath.Join(stage, t["hash"]), data, 0600); err != nil {
				return report, fmt.Errorf("stage write failed")
			}
			fileHashes[t["hash"]] = true
		}
		uuid := model.UUID()
		if _, err = insert("INSERT INTO textures(uuid,account_id,name,type,hash,size_bytes,width,height,is_public,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)", uuid, owner, t["name"], kind, t["hash"], len(data), w, h, integer(t["public"]), oldTime(t["upload_at"], zone, now)); err != nil {
			return report, err
		}
		var id int64
		if err = tx.QueryRowContext(ctx, q("SELECT id FROM textures WHERE uuid=?"), uuid).Scan(&id); err != nil {
			return report, err
		}
		textures[t["tid"]] = id
		textureKinds[t["tid"]] = kind
		textureOwners[t["tid"]] = t["uploader"]
		texturePublic[t["tid"]] = t["public"] == "1"
		report.Counts["textures"]++
	}
	usedOverrides := map[string]bool{}
	for i, p := range tables["players"] {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		advance("players", i)
		owner := users[p["uid"]]
		if owner == 0 {
			report.issue("players", p["pid"], "owner_missing", false)
			continue
		}
		if !identity.ValidPlayer(p["name"]) || existingPlayers[strings.ToLower(p["name"])] {
			report.issue("players", p["pid"], "invalid_or_duplicate_name", true)
			continue
		}
		candidates := map[string]bool{}
		for _, u := range tables["uuid"] {
			if u["pid"] == p["pid"] || strings.EqualFold(u["name"], p["name"]) {
				if uuidPattern.MatchString(u["uuid"]) {
					candidates[strings.ToLower(strings.ReplaceAll(u["uuid"], "-", ""))] = true
				} else {
					report.issue("players", p["pid"], "invalid_uuid_mapping", input.UUIDOverrides[p["pid"]] == "")
				}
			}
		}
		uuid := identity.OfflineUUID(p["name"])
		strategy := model.UUIDOffline
		if len(candidates) > 1 && input.UUIDOverrides[p["pid"]] == "" {
			report.issue("players", p["pid"], "ambiguous_uuid", true)
		}
		if randomUUIDs && len(candidates) == 0 && input.UUIDOverrides[p["pid"]] == "" {
			report.issue("players", p["pid"], "random_uuid_missing", true)
		}
		for candidate := range candidates {
			uuid = candidate
			strategy = model.UUIDPreserved
		}
		if override, ok := input.UUIDOverrides[p["pid"]]; ok {
			usedOverrides[p["pid"]] = true
			if !uuidPattern.MatchString(override) {
				report.issue("players", p["pid"], "invalid_uuid_override", true)
				continue
			}
			uuid = strings.ToLower(strings.ReplaceAll(override, "-", ""))
			strategy = model.UUIDPreserved
		}
		if existingUUIDs[uuid] {
			report.issue("players", p["pid"], "duplicate_uuid", true)
			continue
		}
		existingUUIDs[uuid] = true
		existingPlayers[strings.ToLower(p["name"])] = true
		reference := func(tid string, cape bool) any {
			if textures[tid] == 0 {
				return nil
			}
			validKind := textureKinds[tid] != 2
			if cape {
				validKind = textureKinds[tid] == 2
			}
			if !validKind || (!texturePublic[tid] && textureOwners[tid] != p["uid"]) {
				report.issue("players", p["pid"], "texture_reference_denied", false)
				return nil
			}
			return textures[tid]
		}
		if _, err = insert("INSERT INTO players(uuid,account_id,name,skin_id,cape_id,created_at,updated_at,uuid_strategy) VALUES(?,?,?,?,?,?,?,?)", uuid, owner, p["name"], reference(p["tid_skin"], false), reference(p["tid_cape"], true), oldTime(p["last_modified"], zone, now), oldTime(p["last_modified"], zone, now), strategy); err != nil {
			return report, err
		}
		report.Counts["players"]++
		report.Players = append(report.Players, PlayerMapping{p["pid"], p["name"], uuid})
	}
	for pid := range input.UUIDOverrides {
		if !usedOverrides[pid] {
			report.issue("players", pid, "unused_uuid_override", true)
		}
	}
	seenCloset := map[string]bool{}
	for _, c := range tables["user_closet"] {
		key := c["user_uid"] + ":" + c["texture_tid"]
		if users[c["user_uid"]] == 0 || textures[c["texture_tid"]] == 0 || (!texturePublic[c["texture_tid"]] && textureOwners[c["texture_tid"]] != c["user_uid"]) || seenCloset[key] {
			report.issue("user_closet", "", "missing_private_or_duplicate", false)
			continue
		}
		seenCloset[key] = true
		if len(c["item_name"]) > 50 {
			c["item_name"] = ""
		}
		if _, err = insert("INSERT INTO closet(account_id,texture_id,item_name,created_at) VALUES(?,?,?,?)", users[c["user_uid"]], textures[c["texture_tid"]], c["item_name"], now); err != nil {
			return report, err
		}
		report.Counts["closet"]++
	}
	for _, r := range tables["reports"] {
		tid := textures[r["tid"]]
		uid := users[r["reporter"]]
		if tid == 0 || uid == 0 || len(r["reason"]) > 1000 {
			report.issue("reports", r["id"], "missing_reference_or_invalid", false)
			continue
		}
		state := integer(r["status"])
		if state < 0 || state > 2 {
			state = 0
		}
		dedup := integer(r["id"])
		if state == 0 {
			dedup = 0
		}
		if _, err = insert("INSERT INTO reports(uuid,texture_id,reporter_id,reason,state,dedup_key,created_at) VALUES(?,?,?,?,?,?,?)", model.UUID(), tid, uid, r["reason"], state, dedup, oldTime(r["report_at"], zone, now)); err != nil {
			return report, err
		}
		report.Counts["reports"]++
	}
	if input.ImportSettings {
		policy, err := site.Read(ctx, tx)
		if err != nil {
			return report, err
		}
		for _, o := range tables["options"] {
			v := o["option_value"]
			n := integer(v)
			switch o["option_name"] {
			case "site_name":
				policy.Name = v
			case "site_description":
				policy.Description = v
			case "announcement", "announcement_zh_CN":
				policy.Announcement = v
			case "allow_register":
				policy.Registration = int16(n)
			case "user_initial_score":
				policy.InitialPoints = n
			case "score_per_player":
				policy.PlayerCost = n
			case "score_per_storage":
				policy.UploadCost = n
			case "private_score_per_storage":
				policy.PrivateUploadCost = n
			case "score_per_closet_item":
				policy.ClosetCost = n
			case "sign_gap_time":
				policy.SignHours = int(n)
			case "sign_score":
				var scores []int64
				if json.Unmarshal([]byte("["+v+"]"), &scores) == nil && len(scores) == 2 {
					policy.SignMin = scores[0]
					policy.SignMax = scores[1]
				}
			}
		}
		if policy.Validate() != nil {
			report.issue("options", "", "invalid_settings", true)
		} else if err = site.Write(ctx, tx, e.Dialect, policy); err != nil {
			return report, err
		}
	}
	if report.Blocking > 0 {
		return report, errBlocked
	}
	if initiallyEmpty {
		actor = firstOwner
	}
	if actor != "" {
		if err = audit.Record(ctx, tx, e.Dialect, actor, "", model.AuditBSImport, requestID, now, model.AuditApplied, -1, -1); err != nil {
			return report, err
		}
	}
	advance("verify", len(report.Players))
	// 对每个协议身份逐个核对，不以统计总数代替 UUID 对账。
	for _, p := range report.Players {
		var uuid string
		if err = tx.QueryRowContext(ctx, q("SELECT uuid FROM players WHERE name=?"), p.Name).Scan(&uuid); err != nil || uuid != p.UUID {
			return report, fmt.Errorf("player verification failed")
		}
	}
	if dry {
		return report, nil
	}
	if err = os.MkdirAll(e.TexturesDir, 0700); err != nil {
		return report, fmt.Errorf("storage unavailable")
	}
	for hash := range fileHashes {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		dst := filepath.Join(e.TexturesDir, hash)
		if _, _, _, readErr := sourceFile(e.TexturesDir, hash); readErr == nil {
			continue
		}
		if err = publishFile(filepath.Join(stage, hash), dst); err != nil {
			return report, err
		}
	}
	if err = tx.Commit(); err != nil {
		return report, fmt.Errorf("target commit failed")
	}
	return report, nil
}

func publishFile(source, target string) error {
	// 先复制到目标卷再原子发布，避免失败留下可见的半个 hash 文件。
	f, err := os.CreateTemp(filepath.Dir(target), ".bs-import-*")
	if err != nil {
		return fmt.Errorf("texture stage unavailable")
	}
	defer os.Remove(f.Name())
	src, err := os.Open(source)
	if err != nil {
		f.Close()
		return fmt.Errorf("texture stage unavailable")
	}
	_, copyErr := io.Copy(f, src)
	src.Close()
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		return fmt.Errorf("texture publish failed")
	}
	if err = os.Link(f.Name(), target); err != nil {
		return fmt.Errorf("texture publish conflict")
	}
	return nil
}
