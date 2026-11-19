package dnfbridge

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/adminapi"
)

// 注册限制（2026-11-18 方案 C）：正常注册 = 一机一号 + 一 IP 一号（IP 上限
// DNFBRIDGE_PLAYER_REGISTRATION_LIMIT，默认 1）；网吧等共享出口场景由管理员
// 发放邀请码绕过机器码与 IP 双重限制（用量/有效期可控，注册来源仍留痕）。
// 邀请码与解绑管理走 /api/game/admin 的 registration_* action，由 GM 面板调用。

const registrationInviteAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

var errRegistrationDeviceUsed = errors.New("该电脑已注册过账号；如重装系统或更换硬件，请联系管理员解绑后重新注册")

// ensureRegistrationSchema 建注册限制相关表并幂等补齐老库缺列。
func (a *accountAPIServer) ensureRegistrationSchema(ctx context.Context) error {
	statements := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s_registration_devices (
device VARCHAR(64) PRIMARY KEY,
account_id VARCHAR(128) NOT NULL DEFAULT '',
created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB`, a.table),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s_registration_invites (
code VARCHAR(32) PRIMARY KEY,
max_uses INT NOT NULL DEFAULT 1,
used INT NOT NULL DEFAULT 0,
expires_at DATETIME NULL,
revoked BOOLEAN NOT NULL DEFAULT FALSE,
note VARCHAR(120) NOT NULL DEFAULT '',
created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB`, a.table),
		// 老库 _registration_sources 没有 device/invite 列：幂等补齐（重复执行报 duplicate column，忽略）。
		fmt.Sprintf(`ALTER TABLE %s_registration_sources ADD COLUMN device VARCHAR(64) NOT NULL DEFAULT ''`, a.table),
		fmt.Sprintf(`ALTER TABLE %s_registration_sources ADD COLUMN invite VARCHAR(32) NOT NULL DEFAULT ''`, a.table),
	}
	for _, statement := range statements {
		if _, err := a.db.ExecContext(ctx, statement); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
				continue
			}
			return err
		}
	}
	return nil
}

// takeRegistrationDeviceSlot 一机一号：注册事务内给本机占位；该机器码已有账号则拒绝。
// 空机器码（非新版登录器）跳过，IP 槽兜底。
func (a *accountAPIServer) takeRegistrationDeviceSlot(ctx context.Context, tx *sql.Tx, device string) error {
	if device == "" {
		return nil
	}
	table := a.table + "_registration_devices"
	if _, err := tx.ExecContext(ctx, "INSERT IGNORE INTO "+table+" (device, account_id) VALUES (?, '')", device); err != nil {
		return err
	}
	var existing string
	if err := tx.QueryRowContext(ctx, "SELECT account_id FROM "+table+" WHERE device=? FOR UPDATE", device).Scan(&existing); err != nil {
		return err
	}
	if existing != "" {
		return errRegistrationDeviceUsed
	}
	return nil
}

// consumeRegistrationInvite 在注册事务内原子核销一个邀请码：
// 存在、未作废、未过期、未用完才扣次数，否则拒绝注册。
func (a *accountAPIServer) consumeRegistrationInvite(ctx context.Context, tx *sql.Tx, code string) error {
	table := a.table + "_registration_invites"
	var maxUses, used int
	var expiresAt sql.NullTime
	var revoked bool
	err := tx.QueryRowContext(ctx,
		"SELECT max_uses, used, expires_at, revoked FROM "+table+" WHERE BINARY code=? FOR UPDATE", code).
		Scan(&maxUses, &used, &expiresAt, &revoked)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return errors.New("邀请码无效，请核对后重试")
	case err != nil:
		return err
	}
	if revoked {
		return errors.New("邀请码已作废，请联系管理员")
	}
	if expiresAt.Valid && !time.Now().Before(expiresAt.Time) {
		return errors.New("邀请码已过期，请联系管理员")
	}
	if used >= maxUses {
		return errors.New("邀请码次数已用完，请联系管理员")
	}
	_, err = tx.ExecContext(ctx, "UPDATE "+table+" SET used=used+1 WHERE BINARY code=?", code)
	return err
}

func newRegistrationInviteCode() (string, error) {
	out := make([]byte, 8)
	max := big.NewInt(int64(len(registrationInviteAlphabet)))
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = registrationInviteAlphabet[n.Int64()]
	}
	return string(out), nil
}

// registrationInviteCreate 批量生成邀请码；expiresDays<=0 表示永不过期。
func (a *accountAPIServer) registrationInviteCreate(ctx context.Context, count, maxUses, expiresDays int, note string) ([]string, error) {
	if count < 1 {
		count = 1
	}
	if count > 50 {
		count = 50
	}
	if maxUses < 1 {
		maxUses = 1
	}
	if maxUses > 100 {
		maxUses = 100
	}
	if expiresDays < 0 {
		expiresDays = 0
	}
	if len(note) > 120 {
		note = note[:120]
	}
	var expiresAt any
	if expiresDays > 0 {
		expiresAt = time.Now().AddDate(0, 0, expiresDays)
	}
	table := a.table + "_registration_invites"
	codes := make([]string, 0, count)
	for i := 0; i < count; i++ {
		var code string
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			code, err = newRegistrationInviteCode()
			if err != nil {
				return nil, err
			}
			result, insertErr := a.db.ExecContext(ctx,
				"INSERT IGNORE INTO "+table+" (code, max_uses, expires_at, note) VALUES (?, ?, ?, ?)",
				code, maxUses, expiresAt, note)
			if insertErr != nil {
				return nil, insertErr
			}
			if affected, err := result.RowsAffected(); err == nil && affected > 0 {
				codes = append(codes, code)
				break
			}
		}
		if len(codes) <= i {
			return nil, errors.New("邀请码生成冲突，请重试")
		}
	}
	return codes, nil
}

func (a *accountAPIServer) registrationInviteRevoke(ctx context.Context, code string) error {
	if code == "" {
		return errors.New("邀请码不能为空")
	}
	result, err := a.db.ExecContext(ctx,
		"UPDATE "+a.table+"_registration_invites SET revoked=TRUE WHERE BINARY code=? AND revoked=FALSE", code)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return errors.New("邀请码不存在或已作废")
	}
	return nil
}

func (a *accountAPIServer) registrationInviteList(ctx context.Context, q adminapi.RegistrationQuery) ([]adminapi.RegistrationInvite, int, error) {
	limit, offset := registrationPageBounds(q)
	table := a.table + "_registration_invites"
	var total int
	if err := a.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := a.db.QueryContext(ctx,
		"SELECT code, max_uses, used, expires_at, revoked, note, created_at FROM "+table+
			" ORDER BY created_at DESC, code LIMIT ? OFFSET ?", limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []adminapi.RegistrationInvite{}
	for rows.Next() {
		var item adminapi.RegistrationInvite
		var expiresAt, createdAt sql.NullTime
		if err := rows.Scan(&item.Code, &item.MaxUses, &item.Used, &expiresAt, &item.Revoked, &item.Note, &createdAt); err != nil {
			return nil, 0, err
		}
		if expiresAt.Valid {
			item.ExpiresAt = expiresAt.Time.Format(time.DateTime)
		}
		if createdAt.Valid {
			item.CreatedAt = createdAt.Time.Format(time.DateTime)
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}

// registrationRecords 注册留痕：账号 + 来源 IP/机器码/邀请码，倒序分页。
func (a *accountAPIServer) registrationRecords(ctx context.Context, q adminapi.RegistrationQuery) ([]adminapi.RegistrationRecord, int, error) {
	limit, offset := registrationPageBounds(q)
	sources, accounts := a.table+"_registration_sources", a.table
	var total int
	if err := a.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+sources).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := a.db.QueryContext(ctx,
		"SELECT s.account_id, COALESCE(acc.account, ''), s.ip, s.device, s.invite, acc.created_at "+
			"FROM "+sources+" s LEFT JOIN "+accounts+" acc ON acc.account_id = s.account_id "+
			"ORDER BY s.account_id DESC LIMIT ? OFFSET ?", limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []adminapi.RegistrationRecord{}
	for rows.Next() {
		var item adminapi.RegistrationRecord
		var createdAt sql.NullTime
		if err := rows.Scan(&item.AccountID, &item.Account, &item.IP, &item.Device, &item.Invite, &createdAt); err != nil {
			return nil, 0, err
		}
		if createdAt.Valid {
			item.CreatedAt = createdAt.Time.Format(time.DateTime)
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}

// registrationUnbind 解绑机器码 / 清零 IP 注册计数，放行重新注册。
func (a *accountAPIServer) registrationUnbind(ctx context.Context, change adminapi.RegistrationUnbind) error {
	value := strings.TrimSpace(change.Value)
	if value == "" {
		return errors.New("解绑值不能为空")
	}
	switch change.Kind {
	case "device":
		if !playerDevicePattern.MatchString(value) {
			return errors.New("机器码格式无效")
		}
		result, err := a.db.ExecContext(ctx,
			"DELETE FROM "+a.table+"_registration_devices WHERE device=?", value)
		if err != nil {
			return err
		}
		if affected, err := result.RowsAffected(); err == nil && affected == 0 {
			return errors.New("该机器码没有注册记录")
		}
		return nil
	case "ip":
		parsed := net.ParseIP(value)
		if parsed == nil {
			return errors.New("IP 格式无效")
		}
		result, err := a.db.ExecContext(ctx,
			"UPDATE "+a.table+"_registration_ips SET used=0 WHERE ip=?", parsed.String())
		if err != nil {
			return err
		}
		if affected, err := result.RowsAffected(); err == nil && affected == 0 {
			return errors.New("该 IP 没有注册记录")
		}
		return nil
	default:
		return errors.New("未知的解绑类型（device/ip）")
	}
}

func registrationPageBounds(q adminapi.RegistrationQuery) (int, int) {
	limit, offset := q.Limit, q.Offset
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// handleRegistrationAdmin 是 registration_* 管理 action 的分发入口（game_admin_api.go 调用）。
func (a *accountAPIServer) handleRegistrationAdmin(ctx context.Context, req adminapi.Request, out *adminapi.Response) {
	var err error
	switch req.Action {
	case "registration_invites":
		q := adminapi.RegistrationQuery{}
		if req.RegistrationQuery != nil {
			q = *req.RegistrationQuery
		}
		out.RegistrationInvites, out.RegistrationTotal, err = a.registrationInviteList(ctx, q)
	case "registration_invite_save":
		if req.RegistrationInvite == nil {
			err = errors.New("缺少邀请码参数")
			break
		}
		change := *req.RegistrationInvite
		switch change.Op {
		case "create":
			out.RegistrationCodes, err = a.registrationInviteCreate(ctx, change.Count, change.MaxUses, change.ExpiresDays, change.Note)
		case "revoke":
			err = a.registrationInviteRevoke(ctx, strings.ToUpper(strings.TrimSpace(change.Code)))
		default:
			err = errors.New("未知操作（create/revoke）")
		}
		out.RegistrationChanged = err == nil
	case "registration_records":
		q := adminapi.RegistrationQuery{}
		if req.RegistrationQuery != nil {
			q = *req.RegistrationQuery
		}
		out.RegistrationRecords, out.RegistrationTotal, err = a.registrationRecords(ctx, q)
	case "registration_unbind":
		if req.RegistrationUnbind == nil {
			err = errors.New("缺少解绑参数")
			break
		}
		err = a.registrationUnbind(ctx, *req.RegistrationUnbind)
		out.RegistrationChanged = err == nil
	}
	if err != nil {
		out.OK = false
		out.Error = err.Error()
		if strings.Contains(err.Error(), "sql:") || strings.Contains(err.Error(), "Error ") {
			out.Error = "注册管理服务暂不可用"
		}
	}
}
