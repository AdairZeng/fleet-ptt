package linxchat

import (
	"sync"
	"time"
)

// JoinConfirmTTL 是设备待确认操作的有效时间。
const JoinConfirmTTL = 2 * time.Minute

type pendingJoin struct {
	Code  string
	Until time.Time
}

// JoinGate 为每个设备保存一个待确认操作。
// Offer 只登记内容，只有明确确认才能消费待办。
type JoinGate struct {
	ttl     time.Duration
	mu      sync.Mutex
	pending map[string]pendingJoin
}

// NewJoinGate 使用 ttl 作为复述窗口。
func NewJoinGate(ttl time.Duration) *JoinGate {
	return &JoinGate{ttl: ttl, pending: map[string]pendingJoin{}}
}

// Offer 登记待办；重复内容保留原到期时间，不隐式确认或续期。
func (g *JoinGate) Offer(deviceID, content string, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if p, ok := g.pending[deviceID]; ok && now.Before(p.Until) && p.Code == content {
		return
	}
	g.pending[deviceID] = pendingJoin{Code: content, Until: now.Add(g.ttl)}
}

// Confirm 取出仍有效的待确认口令。没有或已过期时 ok 为 false。
func (g *JoinGate) Confirm(userID string, now time.Time) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	p, ok := g.pending[userID]
	if !ok || !now.Before(p.Until) {
		delete(g.pending, userID)
		return "", false
	}
	delete(g.pending, userID)
	return p.Code, true
}

// Cancel 放弃仍有效的待确认口令。本来就没有时返回 false。
func (g *JoinGate) Cancel(userID string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	p, ok := g.pending[userID]
	delete(g.pending, userID)
	return ok && now.Before(p.Until)
}

// Clear 在司机改说别的指令时丢掉待确认口令，避免过一会儿误入群。
func (g *JoinGate) Clear(userID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.pending, userID)
}

// Pending reads the active device context without consuming confirmation.
func (g *JoinGate) Pending(deviceID string, now time.Time) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	p, ok := g.pending[deviceID]
	if !ok || !now.Before(p.Until) {
		delete(g.pending, deviceID)
		return "", false
	}
	return p.Code, true
}
