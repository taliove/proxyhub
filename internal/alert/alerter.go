package alert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SettingsSource 提供动态读取告警设置的能力（由 store 实现）
type SettingsSource interface {
	GetSetting(key string) (string, error)
}

// Alerter 飞书告警器。webhook 从设置动态读取，无需重启即可修改。
type Alerter struct {
	settings SettingsSource
	client   *http.Client
}

// NewAlerter 创建告警器
func NewAlerter(settings SettingsSource) *Alerter {
	return &Alerter{
		settings: settings,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// webhookURL 从设置读取飞书 webhook，未配置返回空串
func (a *Alerter) webhookURL() string {
	if a.settings == nil {
		return ""
	}
	url, err := a.settings.GetSetting("feishu_webhook")
	if err != nil {
		return ""
	}
	return url
}

// Alert 发送告警消息。未配置 webhook 时静默跳过。
func (a *Alerter) Alert(title, content string) error {
	webhook := a.webhookURL()
	if webhook == "" {
		return nil
	}

	msg := map[string]any{
		"msg_type": "interactive",
		"card": map[string]any{
			"header": map[string]any{
				"title": map[string]string{
					"tag":     "plain_text",
					"content": title,
				},
				"template": "red",
			},
			"elements": []map[string]any{
				{
					"tag": "div",
					"text": map[string]string{
						"tag":     "lark_md",
						"content": content,
					},
				},
				{
					"tag": "hr",
				},
				{
					"tag": "note",
					"elements": []map[string]string{
						{
							"tag":     "plain_text",
							"content": time.Now().Format("2006-01-02 15:04:05"),
						},
					},
				},
			},
		},
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	resp, err := a.client.Post(webhook, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("post to feishu: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("feishu returned status %d", resp.StatusCode)
	}

	return nil
}

// AlertAirportDown 机场全挂告警
func (a *Alerter) AlertAirportDown(airportName string, totalNodes int) error {
	title := "⚠️ 机场节点全部不可用"
	content := fmt.Sprintf("**机场：** %s\n\n**节点总数：** %d\n\n**状态：** 所有节点均不可用",
		airportName, totalNodes)
	return a.Alert(title, content)
}

// AlertLowAvailability 可用节点不足告警
func (a *Alerter) AlertLowAvailability(available, threshold int) error {
	title := "⚠️ 可用节点不足"
	content := fmt.Sprintf("**当前可用节点：** %d\n\n**阈值：** %d\n\n**建议：** 检查机场状态或添加新订阅",
		available, threshold)
	return a.Alert(title, content)
}
