package dnfbridge

// CDK 兑换码：生成（GM，Bearer playerAdminSecret）+ 兑换/角色列表（登录器，账号密码验证）。
//
// 码表存账号 API 库（dnf90_web_* 同库，/pm_cdk_* 前缀），发货走游戏库仓储组：
//   - 点券：dnf_accounts.metadata_json["account_cera"]（cerashop 口径）+ 纯文本通知邮件，
//     与 GM 面板的账号加扣点券、signupbenefits.WelcomeCera 同一存储口径；
//   - 道具/装备：系统邮件附件 {ItemID, Count, Bind}——耐久/品质种子/身份投影/容器路由
//     全部由领取端（op95 → mailbox_pvf.go 的 resolver）按运行时 PVF 自动补齐，
//     发放端只写 itemID 与数量（GM 面板邮件同链）。
//
// 并发防重：兑换先在码表库事务内 SELECT ... FOR UPDATE + INSERT redemption
// （PK (code, account_id)），占用成功后再发货；发货失败回滚占用，玩家可重试。
// 幂等兜底：系统邮件 CompletionKey = "cdk:<code>:<accountID>"，重复投递会被邮件层挡住。

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	dnfcerashop "longheng.io/server/internal/modules/dnf/cerashop"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	cdkCodePrefix     = "PM-"
	cdkCodeAlphabet   = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // 去掉易混淆的 I/O/0/1
	cdkCodeBodyLength = 20                                 // 新格式 5 组×4 位；旧 PM- 码 12 位仍可兑换
	cdkMaxBatch       = 100
	cdkMailSource     = "cdk_redeem"
	cdkMaxCharacter   = 64
)

// 新格式 = XXXX-XXXX-XXXX-XXXX-XXXX；旧格式 = PM-+12 位（兼容网关 GM 面板历史码）。
var (
	cdkNewCodePattern    = regexp.MustCompile(`^[A-HJ-NP-Z2-9]{4}(?:-[A-HJ-NP-Z2-9]{4}){4}$`)
	cdkNewBodyPattern    = regexp.MustCompile(`^[A-HJ-NP-Z2-9]{20}$`)
	cdkLegacyBodyPattern = regexp.MustCompile(`^[A-HJ-NP-Z1-9]{12}$`)
	cdkCodePattern       = regexp.MustCompile(`^(?:[A-HJ-NP-Z2-9]{4}(?:-[A-HJ-NP-Z2-9]{4}){4}|PM-[A-HJ-NP-Z1-9]{12})$`)
)

// cdkNormalizeCode 把玩家输入规整为码表存储形态：
// 大写 → 去空白 → 去连字符 → 20 位回插连字符成 XXXX-…-XXXX；12 位旧码补 PM- 前缀
// （注意先剥 PM- 前缀再剥连字符，否则前缀里的连字符会被误剥）；其余原样返回，
// 交由 cdkCodePattern 校验后报「格式不正确」。
func cdkNormalizeCode(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	s = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '　' {
			return -1
		}
		return r
	}, s)
	body := strings.Map(func(r rune) rune {
		if r == '-' {
			return -1
		}
		return r
	}, strings.TrimPrefix(s, cdkCodePrefix))
	switch {
	case len(body) == 20 && cdkNewBodyPattern.MatchString(body):
		var b strings.Builder
		for i := 0; i < len(body); i += 4 {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteString(body[i : i+4])
		}
		return b.String()
	case len(body) == 12 && cdkLegacyBodyPattern.MatchString(body):
		return cdkCodePrefix + body
	default:
		return s
	}
}

type cdkCodePayload struct {
	Kind      string `json:"kind"` // cera | item | equipment
	Cera      int64  `json:"cera,omitempty"`
	Gold      int64  `json:"gold,omitempty"`
	ItemID    int64  `json:"itemId,omitempty"`
	ItemCount int    `json:"itemCount,omitempty"`
	Bind      bool   `json:"bind,omitempty"`
}

type cdkGenerateRequest struct {
	Kind        string `json:"kind"`
	Cera        int64  `json:"cera"`
	Gold        int64  `json:"gold"`
	ItemID      int64  `json:"itemId"`
	ItemCount   int    `json:"itemCount"`
	Bind        bool   `json:"bind"`
	MaxUses     int    `json:"maxUses"`
	ExpiresDays int    `json:"expiresDays"`
	Count       int    `json:"count"`
	Note        string `json:"note"`
}

type cdkGenerateReply struct {
	OK     bool     `json:"ok"`
	Codes  []string `json:"codes"`
	Errors []string `json:"errors,omitempty"`
}

type cdkRedeemRequest struct {
	Account     string `json:"account"`
	Password    string `json:"password"`
	Code        string `json:"code"`
	CharacterID string `json:"characterId"`
}

type cdkRedeemReply struct {
	OK          bool   `json:"ok"`
	Message     string `json:"message"`
	Kind        string `json:"kind,omitempty"`
	Cera        int64  `json:"cera,omitempty"`
	Gold        int64  `json:"gold,omitempty"`
	ItemID      int64  `json:"itemId,omitempty"`
	ItemCount   int    `json:"itemCount,omitempty"`
	CharacterID string `json:"characterId,omitempty"`
}

type cdkCharacterEntry struct {
	CharacterID string `json:"characterId"`
	Name        string `json:"name"`
	Level       int    `json:"level"`
	Job         string `json:"job"`
}

type cdkCharactersReply struct {
	OK         bool                `json:"ok"`
	Message    string              `json:"message,omitempty"`
	Characters []cdkCharacterEntry `json:"characters"`
}

type cdkAuthRequest struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

// ensureCdkSchema 建码表与核销记录表（幂等，随账号 API schema 一起执行）。
func (a *accountAPIServer) ensureCdkSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS pm_cdk_codes (
			code VARCHAR(32) NOT NULL PRIMARY KEY,
			payload_json TEXT NOT NULL,
			max_uses INT NOT NULL DEFAULT 1,
			used_count INT NOT NULL DEFAULT 0,
			expires_at DATETIME NULL,
			created_at DATETIME NOT NULL,
			note VARCHAR(120) NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS pm_cdk_redemptions (
			code VARCHAR(32) NOT NULL,
			account_id VARCHAR(64) NOT NULL,
			character_id VARCHAR(64) NOT NULL,
			redeemed_at DATETIME NOT NULL,
			PRIMARY KEY (code, account_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	}
	for _, stmt := range stmts {
		if _, err := a.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("cdk schema: %w", err)
		}
	}
	return nil
}

// handleCdkGenerate GM 批量生成兑换码。服务器上直连游戏服调用（网关代理会剥
// Authorization 头，不要走 8443）：
//
//	curl -sk -X POST http://<游戏服IP>:<accountAPI端口>/api/cdk/generate \
//	  -H "Authorization: Bearer <playerAdminSecret>" -H "Content-Type: application/json" \
//	  -d '{"kind":"cera","cera":1000,"maxUses":1,"expiresDays":30,"count":10,"note":"周年庆"}'
func (a *accountAPIServer) handleCdkGenerate(w http.ResponseWriter, r *http.Request) {
	secret := a.service.options.playerAdminSecret
	if len(secret) < 32 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+secret)) != 1 {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req cdkGenerateRequest
	if !decodeCdkBody(w, r, &req) {
		return
	}
	if msg := validateCdkGenerate(&req); msg != "" {
		writeCdkJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": msg})
		return
	}
	if req.Kind != "cera" {
		// 道具/装备必须先过运行时 PVF：查不到的 itemID 直接拒绝，避免生成废码。
		catalog, err := a.service.currentPVFItemCatalog()
		if err != nil {
			writeCdkJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "message": "运行时 PVF 不可用，稍后再试"})
			return
		}
		definition, resolveErr := catalog.ResolveItem(uint32(req.ItemID))
		if resolveErr != nil {
			writeCdkJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": fmt.Sprintf("道具 %d 不在运行时 PVF 中：%v", req.ItemID, resolveErr)})
			return
		}
		if req.Kind == "equipment" && definition.Kind != "equipment" {
			writeCdkJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": fmt.Sprintf("道具 %d 不是装备（PVF 类型 %s）", req.ItemID, definition.Kind)})
			return
		}
		if req.Kind == "item" && definition.Kind == "equipment" {
			// PVF 说它是装备就按装备发：领取端投影按 Kind 走，语义必须一致。
			req.Kind = "equipment"
		}
	}

	expires := sql.NullTime{}
	if req.ExpiresDays > 0 {
		expires = sql.NullTime{Time: time.Now().UTC().AddDate(0, 0, req.ExpiresDays), Valid: true}
	}
	payload, _ := json.Marshal(cdkCodePayload{Kind: req.Kind, Cera: req.Cera, Gold: req.Gold, ItemID: req.ItemID, ItemCount: req.ItemCount, Bind: req.Bind})

	codes := make([]string, 0, req.Count)
	for i := 0; i < req.Count; i++ {
		code := cdkNewCode()
		if _, err := a.db.ExecContext(r.Context(),
			"INSERT INTO pm_cdk_codes (code, payload_json, max_uses, used_count, expires_at, created_at, note) VALUES (?,?,?,0,?,?,?)",
			code, string(payload), req.MaxUses, expires, time.Now().UTC(), strings.TrimSpace(req.Note)); err != nil {
			writeCdkJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "写入码表失败：" + err.Error()})
			return
		}
		codes = append(codes, code)
	}
	a.service.logInfo("cdk codes generated", "count", req.Count, "kind", req.Kind)
	writeCdkJSON(w, http.StatusOK, cdkGenerateReply{OK: true, Codes: codes})
}

// handleCdkRedeem 登录器兑换入口。
func (a *accountAPIServer) handleCdkRedeem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req cdkRedeemRequest
	if !decodeCdkBody(w, r, &req) {
		return
	}
	req.Code = cdkNormalizeCode(req.Code)
	req.CharacterID = strings.TrimSpace(req.CharacterID)
	if !cdkCodePattern.MatchString(req.Code) {
		writeCdkJSON(w, http.StatusBadRequest, cdkRedeemReply{OK: false, Message: "兑换码格式不正确。"})
		return
	}
	if req.CharacterID == "" {
		writeCdkJSON(w, http.StatusBadRequest, cdkRedeemReply{OK: false, Message: "请先选择兑换角色。"})
		return
	}
	accountID, authReply, authStatus := a.verifyCdkAccount(r, req.Account, req.Password)
	if authReply != nil {
		writeCdkJSON(w, authStatus, *authReply)
		return
	}
	reply, status := a.redeemCdkCode(r.Context(), accountID, req.Code, req.CharacterID)
	writeCdkJSON(w, status, reply)
}

// handleCdkCharacters 登录器查询名下角色（兑换面板的角色下拉）。
func (a *accountAPIServer) handleCdkCharacters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req cdkAuthRequest
	if !decodeCdkBody(w, r, &req) {
		return
	}
	accountID, authReply, authStatus := a.verifyCdkAccount(r, req.Account, req.Password)
	if authReply != nil {
		writeCdkJSON(w, authStatus, *authReply)
		return
	}
	group, ok := a.service.repositoryGroup()
	if !ok || group.Character == nil {
		writeCdkJSON(w, http.StatusServiceUnavailable, cdkCharactersReply{OK: false, Message: "角色服务暂不可用，请稍后再试。"})
		return
	}
	records, listErr := group.Character.ListByAccount(r.Context(), accountID, cdkMaxCharacter)
	if listErr != nil {
		writeCdkJSON(w, http.StatusInternalServerError, cdkCharactersReply{OK: false, Message: "读取角色列表失败，请稍后再试。"})
		return
	}
	characters := make([]cdkCharacterEntry, 0, len(records))
	for _, record := range records {
		characters = append(characters, cdkCharacterEntry{CharacterID: record.CharacterID, Name: record.Name, Level: record.Level, Job: record.Job})
	}
	writeCdkJSON(w, http.StatusOK, cdkCharactersReply{OK: true, Characters: characters})
}

// verifyCdkAccount 账号密码验证（bcrypt，与登录同源），成功返回 account_id。
func (a *accountAPIServer) verifyCdkAccount(r *http.Request, account, password string) (string, *cdkRedeemReply, int) {
	account = strings.ToLower(strings.TrimSpace(account))
	if account == "" || password == "" {
		return "", &cdkRedeemReply{OK: false, Message: "请输入账号和密码。"}, http.StatusBadRequest
	}
	ctx, cancel := context.WithTimeout(r.Context(), accountAPIWriteTimeout)
	defer cancel()
	var hash, id string
	queryErr := a.db.QueryRowContext(ctx,
		"SELECT password_hash,account_id FROM "+a.table+" WHERE account=?", account).Scan(&hash, &id)
	if queryErr != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return "", &cdkRedeemReply{OK: false, Message: "账号或密码不正确。"}, http.StatusUnauthorized
	}
	return strings.TrimSpace(id), nil, 0
}

// redeemCdkCode 核销 + 发货。占用（码表库）先行防并发，发货失败回滚占用。
func (a *accountAPIServer) redeemCdkCode(ctx context.Context, accountID, code, characterID string) (cdkRedeemReply, int) {
	ctx, cancel := context.WithTimeout(ctx, accountAPIWriteTimeout)
	defer cancel()

	busy := cdkRedeemReply{OK: false, Message: "服务繁忙，请稍后再试。"}

	// 1) 占用：事务内锁码行 → 校验 → 记核销 → 计数 +1
	tx, txErr := a.db.BeginTx(ctx, nil)
	if txErr != nil {
		return busy, http.StatusInternalServerError
	}
	var payloadRaw string
	var maxUses, usedCount int
	var expires sql.NullTime
	scanErr := tx.QueryRowContext(ctx,
		"SELECT payload_json, max_uses, used_count, expires_at FROM pm_cdk_codes WHERE code=? FOR UPDATE",
		code).Scan(&payloadRaw, &maxUses, &usedCount, &expires)
	if errors.Is(scanErr, sql.ErrNoRows) {
		_ = tx.Rollback()
		return cdkRedeemReply{OK: false, Message: "兑换码不存在。"}, http.StatusBadRequest
	}
	if scanErr != nil {
		_ = tx.Rollback()
		return busy, http.StatusInternalServerError
	}
	if expires.Valid && !time.Now().UTC().Before(expires.Time) {
		_ = tx.Rollback()
		return cdkRedeemReply{OK: false, Message: "兑换码已过期。"}, http.StatusBadRequest
	}
	if usedCount >= maxUses {
		_ = tx.Rollback()
		return cdkRedeemReply{OK: false, Message: "兑换码已被兑换完毕。"}, http.StatusBadRequest
	}
	var payload cdkCodePayload
	if jsonErr := json.Unmarshal([]byte(payloadRaw), &payload); jsonErr != nil {
		_ = tx.Rollback()
		return cdkRedeemReply{OK: false, Message: "兑换码内容损坏，请联系管理员。"}, http.StatusInternalServerError
	}
	if _, insertErr := tx.ExecContext(ctx,
		"INSERT INTO pm_cdk_redemptions (code, account_id, character_id, redeemed_at) VALUES (?,?,?,?)",
		code, accountID, characterID, time.Now().UTC()); insertErr != nil {
		_ = tx.Rollback()
		return cdkRedeemReply{OK: false, Message: "该账号已兑换过此码。"}, http.StatusBadRequest
	}
	if _, updateErr := tx.ExecContext(ctx,
		"UPDATE pm_cdk_codes SET used_count = used_count + 1 WHERE code=?", code); updateErr != nil {
		_ = tx.Rollback()
		return busy, http.StatusInternalServerError
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return busy, http.StatusInternalServerError
	}

	// 2) 发货（游戏库）。失败回滚占用，让玩家可以重试。
	if deliverErr := deliverCdkPayload(ctx, a.service, payload, accountID, characterID, code); deliverErr != nil {
		if _, undoErr := a.db.ExecContext(ctx,
			"DELETE FROM pm_cdk_redemptions WHERE code=? AND account_id=?", code, accountID); undoErr == nil {
			_, _ = a.db.ExecContext(ctx, "UPDATE pm_cdk_codes SET used_count = used_count - 1 WHERE code=?", code)
		}
		return cdkRedeemReply{OK: false, Message: "发货失败：" + deliverErr.Error() + "（兑换未扣减，可重试）"}, http.StatusInternalServerError
	}
	a.service.logInfo("cdk redeemed", "code", code, "account", accountID, "character", characterID, "kind", payload.Kind)
	return cdkRedeemReply{OK: true, Message: "兑换成功，请到游戏内邮箱查收。", Kind: payload.Kind, Cera: payload.Cera, Gold: payload.Gold, ItemID: payload.ItemID, ItemCount: payload.ItemCount, CharacterID: characterID}, http.StatusOK
}

// deliverCdkPayload 按码类型发货。全部经由仓储组的资产所有者事务，在线安全。
func deliverCdkPayload(ctx context.Context, s *Service, payload cdkCodePayload, accountID, characterID, code string) error {
	group, ok := s.repositoryGroup()
	if !ok || group.Mailbox == nil {
		return errors.New("游戏数据库暂不可用")
	}
	now := time.Now().UTC()
	completionKey := fmt.Sprintf("cdk:%s:%s", code, accountID)

	// 角色归属校验：防"兑换到别人角色"。
	characters, listErr := group.Character.ListByAccount(ctx, accountID, cdkMaxCharacter)
	if listErr != nil {
		return errors.New("读取角色失败")
	}
	owned := false
	for _, record := range characters {
		if record.CharacterID == characterID {
			owned = true
			break
		}
	}
	if !owned {
		return errors.New("角色不属于该账号")
	}

	title, body := cdkMailText(payload)
	switch payload.Kind {
	case "cera":
		if payload.Cera <= 0 {
			return errors.New("点券数额无效")
		}
		if err := creditCdkCera(ctx, group, accountID, payload.Cera); err != nil {
			return err
		}
		if _, err := dnfrepo.AppendSystemMail(ctx, group.Mailbox, dnfrepo.SystemMailDelivery{
			RecipientCharacterID: characterID, Title: title, Body: body, Source: cdkMailSource,
			CompletionKey: completionKey, CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("通知邮件写入失败: %w", err)
		}
		return nil
	case "item", "equipment":
		if payload.ItemID <= 0 || payload.ItemCount <= 0 {
			return errors.New("道具参数无效")
		}
		if _, err := dnfrepo.AppendSystemMail(ctx, group.Mailbox, dnfrepo.SystemMailDelivery{
			RecipientCharacterID: characterID, Title: title, Body: body, Source: cdkMailSource,
			CompletionKey: completionKey, CreatedAt: now,
			Attachments: []dnfrepo.MailAttachment{{ItemID: payload.ItemID, Count: int64(payload.ItemCount), Bind: payload.Bind}},
		}); err != nil {
			return fmt.Errorf("邮件写入失败: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("未知兑换类型 %q", payload.Kind)
	}
}

// creditCdkCera 把点券直接记入账号共享钱包（account_cera metadata），
// 与 GM 面板加扣点券、signupbenefits.WelcomeCera 同一存储口径；玩家重新选角后生效。
func creditCdkCera(ctx context.Context, group dnfrepo.Group, accountID string, cera int64) error {
	if group.Account == nil {
		return errors.New("账号仓储不可用")
	}
	account, found, loadErr := group.Account.Load(ctx, accountID)
	if loadErr != nil {
		return fmt.Errorf("账号读取失败: %w", loadErr)
	}
	if !found {
		return errors.New("账号不存在")
	}
	wallet := dnfcerashop.WalletBalance(account)
	if wallet.Cera > dnfcerashop.MaxWalletBalance-cera {
		return errors.New("点券余额将达到客户端上限，请缩减兑换码面额")
	}
	dnfcerashop.SetBalance(&account, wallet.Cera+cera)
	if saveErr := group.Account.Save(ctx, account); saveErr != nil {
		return fmt.Errorf("钱包写入失败: %w", saveErr)
	}
	return nil
}

func cdkMailText(payload cdkCodePayload) (string, string) {
	switch payload.Kind {
	case "cera":
		return "CDK 兑换：点券到账", fmt.Sprintf("您兑换的点券 %d 已直接充入账号共享钱包，重新选择角色后即可在商城看到新余额。", payload.Cera)
	case "equipment":
		return "CDK 兑换：装备", "您兑换的装备已通过附件发放，请在邮箱中领取（领取后自动按其品质与耐久入包）。"
	default:
		return "CDK 兑换：道具", "您兑换的道具已通过附件发放，请在邮箱中领取。"
	}
}

func cdkNewCode() string {
	out := make([]byte, cdkCodeBodyLength)
	for i := range out {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(cdkCodeAlphabet))))
		out[i] = cdkCodeAlphabet[n.Int64()]
	}
	// 5 组×4 位，连字符分隔：XXXX-XXXX-XXXX-XXXX-XXXX
	var b strings.Builder
	for i := 0; i < len(out); i += 4 {
		if i > 0 {
			b.WriteByte('-')
		}
		b.Write(out[i : i+4])
	}
	return b.String()
}

func validateCdkGenerate(req *cdkGenerateRequest) string {
	switch req.Kind {
	case "cera":
		if req.Cera <= 0 {
			return "kind=cera 时 cera 必须大于 0"
		}
	case "item", "equipment":
		if req.ItemID <= 0 {
			return "kind=item/equipment 时 itemId 必须大于 0"
		}
		if req.ItemCount <= 0 {
			return "itemCount 必须大于 0"
		}
	default:
		return "kind 必须是 cera / item / equipment"
	}
	if req.MaxUses <= 0 {
		req.MaxUses = 1
	}
	if req.MaxUses > 100000 {
		return "maxUses 上限 100000"
	}
	if req.Count <= 0 {
		req.Count = 1
	}
	if req.Count > cdkMaxBatch {
		return fmt.Sprintf("单次最多生成 %d 个", cdkMaxBatch)
	}
	if len(req.Note) > 120 {
		return "note 过长（上限 120 字符）"
	}
	return ""
}

func decodeCdkBody(w http.ResponseWriter, r *http.Request, target any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		writeCdkJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "请求格式无效。"})
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		writeCdkJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "请求格式无效。"})
		return false
	}
	return true
}

func writeCdkJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

var _ = strconv.Itoa // 保留：后续列表分页用
