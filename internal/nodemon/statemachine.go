package nodemon

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/taliove/proxyhub/internal/detection"
)

// 告警状态机(ADR 0047 / issue #100):订阅节点监控的通断判定与跳变写回。
//
// 判定:连续 3 次失败判宕(告警一次 + 写回池 Available=false);
// 宕后连续 2 次成功判恢复(恢复通知 + 写回 Available=true);
// 疑似(1~2 次失败)只累计不动作——避免节点在订阅里闪进闪出。
// 宕机节点仍下发(监控集合免疫,issue #101),写回只影响可用性展示与长尾过滤。
//
// 状态驻内存(map,按 用户×节点):重启后计数清零,宕机节点需重新累计 3 次
// 失败才再告警——规格注明并接受的重启后告警延迟。

const (
	downAfterFails = 3 // 连续失败判宕阈值
	upAfterOK      = 2 // 宕后连续成功判恢复阈值
)

// PoolWriter 池状态写回(aggregator.UpdateNodeTestResultForUser 满足;
// mode="quick" 记 DetectionKind=health,与即时 TCP 快检同语义)
type PoolWriter interface {
	UpdateNodeTestResultForUser(userID int64, nodeKey, mode string, available bool, latency int, downMbps, upMbps float64, failReason, failDetail string) bool
}

// SlotNamer 槽位名查询(store 满足):告警文案优先用槽位名(用户认名字不认 key)
type SlotNamer interface {
	SlotNameByNodeKeyForUser(userID int64) (map[string]string, error)
}

type nodeState struct {
	down       bool // 当前是否判宕(已告警)
	consecFail int
	consecOK   int
}

// StateMachine 实现 Listener:消费探测结果,驱动告警与跳变写回
type StateMachine struct {
	slots   SlotNamer
	writer  PoolWriter
	alerter Alerter
	logger  *slog.Logger

	mu     sync.Mutex
	states map[string]*nodeState // key: userID\x00nodeKey
}

// NewStateMachine 创建状态机。deps 全部可得于 main.go 接线处。
func NewStateMachine(slots SlotNamer, writer PoolWriter, alerter Alerter, logger *slog.Logger) *StateMachine {
	return &StateMachine{
		slots:   slots,
		writer:  writer,
		alerter: alerter,
		logger:  logger,
		states:  make(map[string]*nodeState),
	}
}

// OnProbeResult 消费一条探测结果(按用户分发,同一物理节点多用户各自判定)
func (sm *StateMachine) OnProbeResult(t Target, s Sample) {
	key := fmt.Sprintf("%d\x00%s", t.UserID, t.NodeKey)

	sm.mu.Lock()
	st, ok := sm.states[key]
	if !ok {
		st = &nodeState{}
		sm.states[key] = st
	}
	var transition string // "down" / "up" / ""
	if s.OK {
		st.consecOK++
		st.consecFail = 0
		if st.down && st.consecOK >= upAfterOK {
			st.down = false
			st.consecOK = 0
			transition = "up"
		}
	} else {
		st.consecFail++
		st.consecOK = 0
		if !st.down && st.consecFail >= downAfterFails {
			st.down = true
			st.consecFail = 0
			transition = "down"
		}
	}
	sm.mu.Unlock()

	switch transition {
	case "down":
		sm.onDown(t, s)
	case "up":
		sm.onUp(t, s)
	}
}

// displayName 告警用名:槽位名优先(用户认名字),否则裸 node_key
func (sm *StateMachine) displayName(t Target) string {
	if sm.slots != nil {
		if m, err := sm.slots.SlotNameByNodeKeyForUser(t.UserID); err == nil {
			if name, ok := m[t.NodeKey]; ok {
				return name
			}
		} else {
			sm.logger.Warn("lookup slot name failed", "key", t.NodeKey, "error", err)
		}
	}
	return t.NodeKey
}

func (sm *StateMachine) onDown(t Target, s Sample) {
	name := sm.displayName(t)
	if sm.writer != nil {
		if !sm.writer.UpdateNodeTestResultForUser(t.UserID, t.NodeKey, "quick", false, 0, 0, 0,
			detection.FailReasonOther, "monitor: TCP connect failed x3") {
			sm.logger.Warn("monitor writeback missed: node not in pool", "user", t.UserID, "key", t.NodeKey)
		}
	}
	sm.alert("订阅节点宕机",
		fmt.Sprintf("节点「%s」连续 %d 次 TCP 探测失败，已判定宕机。\n节点仍在订阅中下发（免疫），请观察或把名称转移给其他节点。\nnode_key: %s",
			name, downAfterFails, t.NodeKey))
	sm.logger.Warn("monitor node down", "user", t.UserID, "key", t.NodeKey, "name", name)
}

func (sm *StateMachine) onUp(t Target, s Sample) {
	name := sm.displayName(t)
	if sm.writer != nil {
		if !sm.writer.UpdateNodeTestResultForUser(t.UserID, t.NodeKey, "quick", true, s.LatencyMs, 0, 0, "", "") {
			sm.logger.Warn("monitor writeback missed: node not in pool", "user", t.UserID, "key", t.NodeKey)
		}
	}
	sm.alert("订阅节点恢复",
		fmt.Sprintf("节点「%s」连续 %d 次探测成功，已恢复。当前延迟 %dms。\nnode_key: %s",
			name, upAfterOK, s.LatencyMs, t.NodeKey))
	sm.logger.Info("monitor node recovered", "user", t.UserID, "key", t.NodeKey, "name", name)
}

func (sm *StateMachine) alert(title, content string) {
	if sm.alerter == nil {
		return
	}
	if err := sm.alerter.Alert(title, content); err != nil {
		sm.logger.Warn("send monitor alert failed", "title", title, "error", err)
	}
}
