package linxchat

import (
	"sync"
	"time"
)

// JoinConfirmTTL 是口令复述确认的有效时间。超时后要重新听一遍。
const JoinConfirmTTL = 2 * time.Minute

type pendingJoin struct {
	Code  string
	Until time.Time
}

// JoinGate 记住每个用户刚听到、尚未入群的口令。
// 同一口令在有效期内再说一次才执行加入，避免 ASR 听错直接进错群。
type JoinGate struct {
	ttl     time.Duration
	mu      sync.Mutex
	pending map[string]pendingJoin
}

// NewJoinGate 使用 ttl 作为复述窗口。
func NewJoinGate(ttl time.Duration) *JoinGate {
	return &JoinGate{ttl: ttl, pending: map[string]pendingJoin{}}
}

// Offer 登记口令。有效期内再次说出同一口令时返回 true，并清掉登记。
// 口令不同或已过期时返回 false，改为登记新口令。
func (g *JoinGate) Offer(userID, code string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if p, ok := g.pending[userID]; ok && now.Before(p.Until) && p.Code == code {
		delete(g.pending, userID)
		return true
	}
	g.pending[userID] = pendingJoin{Code: code, Until: now.Add(g.ttl)}
	return false
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
