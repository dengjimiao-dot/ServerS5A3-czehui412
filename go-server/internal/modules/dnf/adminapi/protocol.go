// Package adminapi defines the private game-runtime management contract.
// It contains no web panel, database connection, scheduler or game transport.
package adminapi

import (
	"longheng.io/server/internal/modules/dnf/activityrank"
	"longheng.io/server/internal/modules/dnf/moderation"
	"longheng.io/server/internal/modules/dnf/onlineevent"
	"longheng.io/server/internal/modules/dnf/qqbinding"
	"longheng.io/server/internal/modules/dnf/repository"
)

const Version = 1
const Path = "/api/game/admin"

// ActionNotifyMailbox projects a mailbox already committed by another process.
// Offline recipients are covered by the selected-town bootstrap.
const ActionNotifyMailbox = "notify_mailbox"

type Request struct {
	QQChange        *qqbinding.Change                  `json:"qqChange,omitempty"`
	QQCheck         *qqbinding.CheckResult             `json:"qqCheck,omitempty"`
	ActivityQuery   *repository.ActivityQuery          `json:"activityQuery,omitempty"`
	PasswordReset   *PasswordReset                     `json:"passwordReset,omitempty"`
	QQAck           *moderation.QQAck                  `json:"qqAck,omitempty"`
	QQ              string                             `json:"qq,omitempty"`
	QQSettings      *qqbinding.Settings                `json:"qqSettings,omitempty"`
	QQComplete      *qqbinding.Complete                `json:"qqComplete,omitempty"`
	QQQuery         *qqbinding.Query                   `json:"qqQuery,omitempty"`
	ModerationQuery *moderation.Query                  `json:"moderationQuery,omitempty"`
	BanChange       *moderation.BanChange              `json:"banChange,omitempty"`
	AccountID       string                             `json:"accountId,omitempty"`
	Version         int                                `json:"version"`
	Action          string                             `json:"action"`
	CharacterID     uint16                             `json:"characterId,omitempty"`
	Revision        uint64                             `json:"revision,omitempty"`
	Changes         []onlineevent.ControlChange        `json:"changes,omitempty"`
	ListType        byte                               `json:"listType,omitempty"`
	Slot            int16                              `json:"slot,omitempty"`
	Stack           *repository.ItemStack              `json:"stack,omitempty"`
	RankQuery       *activityrank.Query                `json:"rankQuery,omitempty"`
	RankSettlement  *repository.ActivityRankSettlement `json:"rankSettlement,omitempty"`
	// 注册限制管理（registration_* action）：邀请码增删查 + 注册记录 + 机器码/IP 解绑。
	RegistrationQuery   *RegistrationQuery      `json:"registrationQuery,omitempty"`
	RegistrationInvite  *RegistrationInviteChange `json:"registrationInvite,omitempty"`
	RegistrationUnbind  *RegistrationUnbind     `json:"registrationUnbind,omitempty"`
	// 深渊史诗掉率动态覆盖：nil=仅读取当前值；非 nil=设置（0..1000，-1 清除回退 PVF）。
	HellDropRate *int `json:"hellDropRate,omitempty"`
}

// HellDropRateStatus 是 GM 面板展示的掉率状态。
type HellDropRateStatus struct {
	Rate        int  `json:"rate"`
	Override    bool `json:"override"`
	PVFBaseline int  `json:"pvfBaseline"`
	Scale       int  `json:"scale"`
}

type Response struct {
	QQChecks           []qqbinding.Check                         `json:"qqChecks,omitempty"`
	QQChanged          bool                                      `json:"qqChanged,omitempty"`
	PasswordReset      *PasswordResetResult                      `json:"passwordReset,omitempty"`
	ErrorCode          string                                    `json:"errorCode,omitempty"`
	BanTargets         *moderation.BanTargets                    `json:"banTargets,omitempty"`
	QQActions          []moderation.QQAction                     `json:"qqActions,omitempty"`
	QQBlocked          bool                                      `json:"qqBlocked,omitempty"`
	QQSettings         *qqbinding.Settings                       `json:"qqSettings,omitempty"`
	QQBinding          *qqbinding.Binding                        `json:"qqBinding,omitempty"`
	QQBindings         *qqbinding.Page                           `json:"qqBindings,omitempty"`
	Clears             *moderation.ClearPage                     `json:"clears,omitempty"`
	Bans               *moderation.BanPage                       `json:"bans,omitempty"`
	Ban                *moderation.Ban                           `json:"ban,omitempty"`
	ModerationAccounts *moderation.AccountPage                   `json:"moderationAccounts,omitempty"`
	CharacterEdit      *CharacterEditStatus                      `json:"characterEdit,omitempty"`
	AccountEdit        *AccountEditStatus                        `json:"accountEdit,omitempty"`
	OnlineSnapshot     *OnlineSnapshot                           `json:"onlineSnapshot,omitempty"`
	ActivitySummary    *repository.ActivitySummary               `json:"activitySummary,omitempty"`
	ActivityEvents     []repository.ActivityEvent                `json:"activityEvents,omitempty"`
	Version            int                                       `json:"version"`
	OK                 bool                                      `json:"ok"`
	Error              string                                    `json:"error,omitempty"`
	Online             bool                                      `json:"online"`
	Activities         *onlineevent.ControlSnapshot              `json:"activities,omitempty"`
	PremiumTypes       []int64                                   `json:"premiumTypes,omitempty"`
	CrystalCubeItemIDs [6]int64                                  `json:"crystalCubeItemIds,omitempty"`
	StackRow           []byte                                    `json:"stackRow,omitempty"`
	RankBoard          *activityrank.Board                       `json:"rankBoard,omitempty"`
	RankSnapshot       *repository.ActivityRankSnapshot          `json:"rankSnapshot,omitempty"`
	RankCreated        bool                                      `json:"rankCreated,omitempty"`
	RankRewards        []repository.ActivityRankReward           `json:"rankRewards,omitempty"`
	GoldSummary        *repository.ActivityGoldSummary           `json:"goldSummary,omitempty"`
	CharacterGold      map[string]repository.ActivityGoldSummary `json:"characterGold,omitempty"`
	// 注册限制管理结果。
	RegistrationInvites []RegistrationInvite `json:"registrationInvites,omitempty"`
	RegistrationRecords []RegistrationRecord `json:"registrationRecords,omitempty"`
	RegistrationCodes   []string             `json:"registrationCodes,omitempty"`
	RegistrationChanged bool                 `json:"registrationChanged,omitempty"`
	RegistrationTotal   int                  `json:"registrationTotal,omitempty"`
	HellDropRate        *HellDropRateStatus  `json:"hellDropRate,omitempty"`
}

// RegistrationQuery 分页查询邀请码 / 注册记录。
type RegistrationQuery struct {
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
}

// RegistrationInviteChange：op=create 批量生成（Count/MaxUses/ExpiresDays/Note），
// op=revoke 作废指定 Code。
type RegistrationInviteChange struct {
	Op          string `json:"op,omitempty"`
	Code        string `json:"code,omitempty"`
	Count       int    `json:"count,omitempty"`
	MaxUses     int    `json:"maxUses,omitempty"`
	ExpiresDays int    `json:"expiresDays,omitempty"`
	Note        string `json:"note,omitempty"`
}

// RegistrationUnbind：Kind=device 删除机器码占位（重装系统/换机后放行重新注册），
// Kind=ip 清零该 IP 的注册计数。
type RegistrationUnbind struct {
	Kind  string `json:"kind,omitempty"`
	Value string `json:"value,omitempty"`
}

type RegistrationInvite struct {
	Code      string `json:"code"`
	MaxUses   int    `json:"maxUses"`
	Used      int    `json:"used"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	Revoked   bool   `json:"revoked,omitempty"`
	Note      string `json:"note,omitempty"`
	CreatedAt string `json:"createdAt"`
}

type RegistrationRecord struct {
	AccountID string `json:"accountId"`
	Account   string `json:"account,omitempty"`
	IP        string `json:"ip,omitempty"`
	Device    string `json:"device,omitempty"`
	Invite    string `json:"invite,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}
