package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/AdairZeng/fleet-ptt/server/internal/channel"
	"github.com/AdairZeng/fleet-ptt/server/internal/user"
)

const (
	sendBuffer = 64
	writeWait  = 5 * time.Second
)

// Hub 保存在线连接，并把对讲状态广播给同一频道的成员。
type Hub struct {
	users    *user.Service
	channels *channel.Service

	mu    sync.Mutex
	conns map[string]map[*client]struct{}
}

// NewHub 用同一套用户与频道服务处理信令。
func NewHub(users *user.Service, channels *channel.Service) *Hub {
	return &Hub{
		users:    users,
		channels: channels,
		conns:    map[string]map[*client]struct{}{},
	}
}

type client struct {
	userID string
	conn   *websocket.Conn
	send   chan []byte

	mu     sync.Mutex
	closed bool
}

func (c *client) sendMsg(payload []byte) bool {
	if len(payload) == 0 {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	select {
	case c.send <- payload:
		return true
	default:
		return false
	}
}

func (c *client) closeSend() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.send)
}

func (c *client) writeLoop() {
	for payload := range c.send {
		ctx, cancel := context.WithTimeout(context.Background(), writeWait)
		err := c.conn.Write(ctx, websocket.MessageText, payload)
		cancel()
		if err != nil {
			_ = c.conn.CloseNow()
			return
		}
	}
}

// ServeHTTP 升级 GET /api/v1/ws。
// 浏览器不能自定义握手头，所以除 Bearer 外也接受 ?token=。
// 凭证是 session_token，不靠 cookie，因此不校验 Origin（开发时 Vite 与 API 不同源）。
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := tokenFromRequest(r)
	if token == "" {
		writeUnauthorized(w)
		return
	}
	u, err := h.users.Authenticate(r.Context(), token)
	if err != nil {
		writeUnauthorized(w)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		log.Printf("ws: accept: %v", err)
		return
	}
	conn.SetReadLimit(1 << 16)
	c := &client{
		userID: u.ID,
		conn:   conn,
		send:   make(chan []byte, sendBuffer),
	}
	h.add(c)
	defer func() {
		h.detach(c)
		c.closeSend()
		_ = conn.CloseNow()
	}()
	go c.writeLoop()

	h.mu.Lock()
	h.greet(r.Context(), c)
	h.mu.Unlock()
	h.readLoop(c)
}

// Create 创建频道。创建者在线时收到 channel_created。
// 若此人正在别的频道对讲，先结束并广播 talk_end。
func (h *Hub) Create(ctx context.Context, userID, name string, now time.Time) (channel.View, error) {
	ctx = context.WithoutCancel(ctx)
	h.mu.Lock()
	defer h.mu.Unlock()

	prevID, wasTalking, err := h.currentTalk(ctx, userID)
	if err != nil {
		return channel.View{}, err
	}
	if wasTalking {
		if err := h.channels.SetTalking(ctx, userID, false); err != nil {
			return channel.View{}, err
		}
	}
	view, err := h.channels.Create(ctx, userID, name, now)
	if err != nil {
		if wasTalking {
			if restoreErr := h.channels.SetTalking(ctx, userID, true); restoreErr != nil {
				log.Printf("ws: restore talking: %v", restoreErr)
			}
		}
		return channel.View{}, err
	}
	if wasTalking && prevID != "" {
		h.publishTalk(ctx, prevID, userID, false)
	}
	h.enqueueUser(userID, encode(channelCreatedWire{
		Type:      "channel_created",
		ChannelID: view.Channel.ID,
		Name:      view.Channel.Name,
		JoinCode:  view.Channel.JoinCode,
	}))
	h.publishMembers(ctx, view.Channel.ID)
	return view, nil
}

// Join 加入频道。当前频道有变化时给该用户推 channel_switched，包括从没有频道变成进入频道。
// 语音入群靠这条信令让 H5 进房。离开正在对讲的频道时，先广播 talk_end。
func (h *Hub) Join(ctx context.Context, userID, code, name string) (channel.View, error) {
	ctx = context.WithoutCancel(ctx)
	h.mu.Lock()
	defer h.mu.Unlock()

	prevID, wasTalking, err := h.currentTalk(ctx, userID)
	if err != nil {
		return channel.View{}, err
	}
	view, err := h.channels.Join(ctx, userID, code, name)
	if err != nil {
		return channel.View{}, err
	}
	if prevID != view.Channel.ID {
		if wasTalking && prevID != "" {
			if err := h.channels.SetTalking(ctx, userID, false); err != nil {
				return channel.View{}, err
			}
			h.publishTalk(ctx, prevID, userID, false)
		}
		h.enqueueUser(userID, encode(channelSwitchedWire{
			Type:      "channel_switched",
			ChannelID: view.Channel.ID,
			Name:      view.Channel.Name,
		}))
	}
	h.publishMembers(ctx, view.Channel.ID)
	return view, nil
}

// Switch 切换当前频道。正在对讲时先广播 talk_end，再通知该用户 channel_switched。
// 已经在目标频道时不发信令，也不结束对讲。
func (h *Hub) Switch(ctx context.Context, userID, channelID string) (channel.View, error) {
	ctx = context.WithoutCancel(ctx)
	h.mu.Lock()
	defer h.mu.Unlock()

	result, err := h.channels.Switch(ctx, userID, channelID)
	if err != nil {
		return channel.View{}, err
	}
	if !result.Changed {
		return result.View, nil
	}
	if result.StoppedTalking && result.PreviousID != "" {
		h.publishTalk(ctx, result.PreviousID, userID, false)
	}
	h.enqueueUser(userID, encode(channelSwitchedWire{
		Type:      "channel_switched",
		ChannelID: result.View.Channel.ID,
		Name:      result.View.Channel.Name,
	}))
	h.publishMembers(ctx, result.View.Channel.ID)
	return result.View, nil
}

// StartTalk 把当前频道标为正在对讲，并向成员广播 talk_start 与 members。
func (h *Hub) StartTalk(ctx context.Context, userID string) (channel.View, error) {
	ctx = context.WithoutCancel(ctx)
	h.mu.Lock()
	defer h.mu.Unlock()

	view, err := h.channels.Current(ctx, userID)
	if err != nil {
		return channel.View{}, err
	}
	if err := h.channels.SetTalking(ctx, userID, true); err != nil {
		return channel.View{}, err
	}
	h.publishTalk(ctx, view.Channel.ID, userID, true)
	return view, nil
}

// PushText 把一句提示发给该用户的连接。typ 为 notice 或 error，供页面提示。
func (h *Hub) PushText(userID, typ, message string) {
	if userID == "" || message == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.enqueueUser(userID, encode(errorWire{Type: typ, Message: message}))
}

// EndTalk 结束当前用户的对讲。已经不在讲时不重复广播。
func (h *Hub) EndTalk(ctx context.Context, userID string) (channel.View, error) {
	ctx = context.WithoutCancel(ctx)
	h.mu.Lock()
	defer h.mu.Unlock()

	view, err := h.channels.Current(ctx, userID)
	if err != nil {
		return channel.View{}, err
	}
	talking, err := h.channels.Talking(ctx, userID)
	if err != nil {
		return channel.View{}, err
	}
	if !talking {
		return view, nil
	}
	if err := h.channels.SetTalking(ctx, userID, false); err != nil {
		return channel.View{}, err
	}
	h.publishTalk(ctx, view.Channel.ID, userID, false)
	return view, nil
}

func (h *Hub) readLoop(c *client) {
	for {
		kind, data, err := c.conn.Read(context.Background())
		if err != nil {
			return
		}
		if kind != websocket.MessageText {
			h.fail(c, "请求格式无效")
			continue
		}
		h.dispatch(c, data)
	}
}

func (h *Hub) dispatch(c *client, data []byte) {
	var msg struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &msg); err != nil || msg.Type == "" {
		h.fail(c, "请求格式无效")
		return
	}
	switch msg.Type {
	case "ping":
		h.reply(c, pongWire{Type: "pong"})
	case "talk_end":
		if _, err := h.EndTalk(context.Background(), c.userID); err != nil {
			if !errors.Is(err, channel.ErrNoCurrent) {
				log.Printf("ws: talk_end: %v", err)
			}
			h.fail(c, talkClientMessage(err))
		}
	default:
		h.fail(c, "无法识别的信令")
	}
}

func (h *Hub) fail(c *client, message string) {
	h.reply(c, errorWire{Type: "error", Message: message})
}

func (h *Hub) reply(c *client, v any) {
	payload := encode(v)
	h.mu.Lock()
	defer h.mu.Unlock()
	if !c.sendMsg(payload) {
		log.Printf("ws: drop event user %s", c.userID)
	}
}

func (h *Hub) add(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.conns[c.userID]
	if set == nil {
		set = map[*client]struct{}{}
		h.conns[c.userID] = set
	}
	set[c] = struct{}{}
}

func (h *Hub) detach(c *client) {
	h.mu.Lock()
	last := h.remove(c)
	h.mu.Unlock()
	if !last {
		return
	}
	if _, err := h.EndTalk(context.Background(), c.userID); err != nil && !errors.Is(err, channel.ErrNoCurrent) {
		log.Printf("ws: end talk after disconnect: %v", err)
	}
}

func (h *Hub) remove(c *client) bool {
	set := h.conns[c.userID]
	if _, ok := set[c]; !ok {
		return false
	}
	delete(set, c)
	if len(set) > 0 {
		return false
	}
	delete(h.conns, c.userID)
	return true
}

func (h *Hub) greet(ctx context.Context, c *client) {
	view, err := h.channels.Current(ctx, c.userID)
	if err != nil {
		if !errors.Is(err, channel.ErrNoCurrent) {
			log.Printf("ws: greet: %v", err)
		}
		return
	}
	members, err := h.channels.Members(ctx, view.Channel.ID)
	if err != nil {
		log.Printf("ws: greet members: %v", err)
		return
	}
	if !c.sendMsg(encode(membersWire{
		Type:      "members",
		ChannelID: view.Channel.ID,
		Members:   wireMembers(members),
	})) {
		log.Printf("ws: drop greet user %s", c.userID)
	}
}

func (h *Hub) currentTalk(ctx context.Context, userID string) (string, bool, error) {
	view, err := h.channels.Current(ctx, userID)
	if errors.Is(err, channel.ErrNoCurrent) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	talking, err := h.channels.Talking(ctx, userID)
	if errors.Is(err, channel.ErrNoCurrent) {
		return view.Channel.ID, false, nil
	}
	if err != nil {
		return "", false, err
	}
	return view.Channel.ID, talking, nil
}

func (h *Hub) publishTalk(ctx context.Context, channelID, userID string, start bool) {
	members, err := h.channels.Members(ctx, channelID)
	if err != nil {
		log.Printf("ws: members %s: %v", channelID, err)
		return
	}
	kind := "talk_end"
	if start {
		kind = "talk_start"
	}
	ids := memberIDs(members)
	h.enqueue(ids, encode(talkWire{Type: kind, ChannelID: channelID, UserID: userID}))
	h.enqueue(ids, encode(membersWire{
		Type:      "members",
		ChannelID: channelID,
		Members:   wireMembers(members),
	}))
}

func (h *Hub) publishMembers(ctx context.Context, channelID string) {
	members, err := h.channels.Members(ctx, channelID)
	if err != nil {
		log.Printf("ws: members %s: %v", channelID, err)
		return
	}
	h.enqueue(memberIDs(members), encode(membersWire{
		Type:      "members",
		ChannelID: channelID,
		Members:   wireMembers(members),
	}))
}

func (h *Hub) enqueueUser(userID string, payload []byte) {
	h.enqueue([]string{userID}, payload)
}

func (h *Hub) enqueue(ids []string, payload []byte) {
	if len(payload) == 0 {
		return
	}
	seen := make(map[*client]struct{}, len(ids))
	for _, id := range ids {
		for c := range h.conns[id] {
			if _, ok := seen[c]; ok {
				continue
			}
			seen[c] = struct{}{}
			if !c.sendMsg(payload) {
				log.Printf("ws: drop event user %s", id)
			}
		}
	}
}

func tokenFromRequest(r *http.Request) string {
	if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
		return token
	}
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.Contains(token, " ") {
		return ""
	}
	return token
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusUnauthorized)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]string{"error": user.ErrUnauthorized.Error()})
}

func talkClientMessage(err error) string {
	if errors.Is(err, channel.ErrNoCurrent) {
		return err.Error()
	}
	return "更新对讲状态失败"
}
