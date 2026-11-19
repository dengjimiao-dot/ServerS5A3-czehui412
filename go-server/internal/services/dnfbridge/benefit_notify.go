package dnfbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// benefitNotifier 在注册账号 / 创建角色成功后通知 GM 网关做一次即时福利投递。
//
// 为什么要事件触发：GM 福利规则的兜底扫描是 3 小时一轮、每页 5 个账号，
// 新注册账号排在队尾，玩家要等很久才收到点券和新手道具。这里在建角色
// 落库后立刻通知 GM（账号级点券也挂在角色邮箱上投递，所以建角色是主入口）。
//
// 配置留空 = 关闭（新增部署才有该项，既有部署行为不变）。调用是异步的，
// 失败只记日志，绝不影响注册 / 建角色结果。
type benefitNotifier struct {
	url    string
	secret string
	client *http.Client
	logf   func(format string, args ...any)
}

func newBenefitNotifier(url, secret string, logf func(string, ...any)) *benefitNotifier {
	if url == "" || len(secret) < 16 {
		return nil
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &benefitNotifier{
		url:    url,
		secret: secret,
		client: &http.Client{Timeout: 8 * time.Second},
		logf:   logf,
	}
}

// Notify 异步触发一次福利投递；characterID 为空表示只按账号投递（注册瞬间）。
func (n *benefitNotifier) Notify(accountID, characterID string) {
	if n == nil || accountID == "" {
		return
	}
	go func() {
		payload, err := json.Marshal(map[string]string{"account": accountID, "character": characterID})
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(payload))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+n.secret)
		resp, err := n.client.Do(req)
		if err != nil {
			n.logf("benefit-notify-failed account=%s character=%s error=%v", accountID, characterID, err)
			return
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			n.logf("benefit-notify-status account=%s character=%s status=%d", accountID, characterID, resp.StatusCode)
		}
	}()
}
