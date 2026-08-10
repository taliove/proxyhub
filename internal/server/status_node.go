package server

import (
	"fmt"
	"strings"
	"time"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// 虚拟状态节点(ADR 0047 / issue #102):订阅输出第一位注入的合成哑节点,
// 名称动态展示该地址下发集合的监控摘要——用户打开客户端就能看到
// "节点还在不在",无需登管理后台。
//
// 形态:合法但无害的 ss 配置(指向 127.0.0.1),所有支持格式(Clash YAML/
// 分享链接)都能渲染导入,客户端永远不会真用它跑流量——机场圈展示
// "剩余流量/到期时间"信息节点就是这套做法。
//
// 状态口径:跟随节点当前检测状态(Available/Unchecked),与监控开关无关——
// 但注意:监控关闭时下发集合已滤掉死节点,状态节点恒显"n/n 在线";
// 故障名单仅在宕机免疫生效(监控开启,issue #101)时非空。开关只控制注不注入。

// maxStatusDownNames 故障名单展示上限:超出以"等 N 个"收拢——名单会经模板
// {{nodes}} 展开复制进每个 proxy-group,无界拼接会让订阅体积随故障数膨胀。
const maxStatusDownNames = 3

const (
	statusNodeServer   = "127.0.0.1"
	statusNodePort     = 19000
	statusNodeCipher   = "aes-128-gcm"
	statusNodePassword = "proxyhub-status"
)

// buildStatusNode 按当前下发集合构造状态节点。down = 确认死亡(检测过且
// 不可用);未检测(Unchecked)按在线计(与 FilterAvailable 放行口径一致)。
// 更新时间取集合内最近检测时刻,全未检测取当前时间。
func buildStatusNode(nodes []*subscription.Node) *subscription.Node {
	total := len(nodes)
	downNames := []string{}
	var latest time.Time
	for _, n := range nodes {
		if !n.Available && !n.Unchecked() {
			downNames = append(downNames, n.EffectiveName())
		}
		if n.DetectionLastCheck.After(latest) {
			latest = n.DetectionLastCheck
		}
	}
	if latest.IsZero() {
		latest = time.Now()
	}
	stamp := latest.Format("15:04")

	var name string
	if len(downNames) == 0 {
		name = fmt.Sprintf("📡 节点状态:%d/%d 在线 · 更新于 %s", total, total, stamp)
	} else {
		shown := downNames
		suffix := ""
		if len(downNames) > maxStatusDownNames {
			shown = downNames[:maxStatusDownNames]
			suffix = fmt.Sprintf(" 等 %d 个", len(downNames))
		}
		name = fmt.Sprintf("⚠️ 节点状态:故障 %d/%d（%s%s）· 更新于 %s",
			len(downNames), total, strings.Join(shown, "、"), suffix, stamp)
	}

	return &subscription.Node{
		Name:     name,
		Type:     "ss",
		Server:   statusNodeServer,
		Port:     statusNodePort,
		Cipher:   statusNodeCipher,
		Password: statusNodePassword,
	}
}

// prependStatusNode 开关开启时在输出最前注入状态节点;关闭原样返回(零回归)。
// 调用点必须在空池 503 判定之后:状态节点不应把空订阅伪装成非空。
func (s *Server) prependStatusNode(nodes []*subscription.Node, ep *store.Endpoint) []*subscription.Node {
	if !ep.StatusNodeEnabled {
		return nodes
	}
	return append([]*subscription.Node{buildStatusNode(nodes)}, nodes...)
}
