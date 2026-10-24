package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/AdairZeng/fleet-ptt/server/internal/channel"
	"github.com/AdairZeng/fleet-ptt/server/internal/linxchat"
	"github.com/AdairZeng/fleet-ptt/server/internal/mcp"
	"github.com/AdairZeng/fleet-ptt/server/internal/store"
	"github.com/AdairZeng/fleet-ptt/server/internal/user"
	"github.com/AdairZeng/fleet-ptt/server/internal/ws"
)

// newLinxMCP exposes business capabilities to the Linx agent. Device identity is
// supplied by the authenticated transport, never by model-generated arguments.
func newLinxMCP(users *user.Service, channels *channel.Service, hub *ws.Hub, token, agentID string) *mcp.Server {
	gate := linxchat.NewJoinGate(linxchat.JoinConfirmTTL)
	var mu sync.Mutex
	active := make(map[string]bool)
	return mcp.New(mcp.Config{Token: token, AgentID: agentID}, linxTools(), func(ctx context.Context, identity mcp.Identity, name string, arguments json.RawMessage) (mcp.ToolResult, error) {
		if identity.DeviceID == "" || identity.SessionID == "" || identity.AgentID != agentID || agentID == "" {
			return toolAnswer("缺少有效的灵汐设备会话身份", true), nil
		}
		command, err := linxCommand(name, arguments)
		if err != nil {
			return toolAnswer(err.Error(), true), nil
		}
		u, err := users.Store.UserByMacID(ctx, identity.DeviceID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return toolAnswer("设备尚未注册或未设置昵称，请先完成设备开机流程", true), nil
			}
			log.Printf("linx mcp: resolve device: %v", err)
			return toolAnswer("读取设备失败", true), nil
		}
		// Serialise even across MCP connections: a second operation must not
		// consume a confirmation or race a device action awaiting acknowledgement.
		mu.Lock()
		if active[u.DeviceID] || len(active) >= 256 {
			mu.Unlock()
			return toolAnswer("设备操作正在执行，请等待结果后重试", true), nil
		}
		active[u.DeviceID] = true
		mu.Unlock()
		defer func() {
			mu.Lock()
			delete(active, u.DeviceID)
			mu.Unlock()
		}()
		exec := voiceExec{channels: channels, hub: hub, gate: gate, sessionID: identity.SessionID}
		result := exec.run(ctx, u, command, time.Now())
		return toolAnswer(result.Answer, result.Failed), nil
	})
}

func toolAnswer(answer string, failed bool) mcp.ToolResult {
	return mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: answer}}, IsError: failed}
}

func linxTools() []mcp.Tool {
	name := map[string]any{"type": "string", "minLength": 1, "maxLength": 32, "description": "名称，不含控制字符"}
	code := map[string]any{"type": "string", "pattern": "^[23456789ABCDEFGHJKLMNPQRSTUVWXYZ]{6}$", "description": "六位大写频道口令，排除 0、1、O、I"}
	index := map[string]any{"type": "integer", "minimum": 1, "description": "list_my_channels 列表中的序号，从 1 开始"}
	level := map[string]any{"type": "integer", "minimum": 0, "maximum": 100}
	adjustment := map[string]any{"type": "string", "enum": []string{"up", "down"}, "description": "音量增加或减少 10"}
	return []mcp.Tool{
		{Name: "create_channel", Description: "创建并进入频道。缺少名称时先询问用户。", InputSchema: toolSchema(map[string]any{"name": name}, "name")},
		{Name: "join_channel", Description: "按口令或频道名称加入频道，二选一。口令加入先返回待确认内容，须等待用户下一次明确确认后调用 confirm_action。", InputSchema: exclusiveToolSchema(map[string]any{"code": code, "name": name}, "code", "name")},
		{Name: "switch_channel", Description: "切换已加入的频道，按名称或列表序号二选一。", InputSchema: exclusiveToolSchema(map[string]any{"name": name, "index": index}, "name", "index")},
		{Name: "previous_channel", Description: "回到上一频道。", InputSchema: toolSchema(nil)},
		{Name: "confirm_action", Description: "仅在用户对上一次复述的口令或昵称明确表示确认后调用。禁止在发起待确认操作的同一轮自动调用。", InputSchema: toolSchema(nil)},
		{Name: "cancel_action", Description: "取消本次会话中尚未确认的操作。", InputSchema: toolSchema(nil)},
		{Name: "talk_start", Description: "开始向当前频道发送语音，等待设备执行结果。", InputSchema: toolSchema(nil)},
		{Name: "talk_end", Description: "结束向当前频道发送语音。", InputSchema: toolSchema(nil)},
		{Name: "list_my_channels", Description: "读取当前设备加入的频道及当前频道。", InputSchema: toolSchema(nil)},
		{Name: "list_speakers", Description: "读取当前频道正在讲话的成员。", InputSchema: toolSchema(nil)},
		{Name: "show_join_code", Description: "读取当前频道的加入口令。", InputSchema: toolSchema(nil)},
		{Name: "show_join_qr", Description: "在设备上显示当前频道的加入二维码，等待设备确认。", InputSchema: toolSchema(nil)},
		{Name: "set_volume", Description: "设置设备音量：0 到 100 的绝对值，或增减 10，二选一。", InputSchema: exclusiveToolSchema(map[string]any{"level": level, "adjustment": adjustment}, "level", "adjustment")},
		{Name: "set_nickname", Description: "提出修改设备昵称并复述。须等待用户下一次明确确认后调用 confirm_action 才会保存。", InputSchema: toolSchema(map[string]any{"name": name}, "name")},
	}
}

func toolSchema(properties map[string]any, required ...string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func exclusiveToolSchema(properties map[string]any, first, second string) map[string]any {
	schema := toolSchema(properties)
	schema["oneOf"] = []any{map[string]any{"required": []string{first}}, map[string]any{"required": []string{second}}}
	return schema
}

// linxCommand validates actual tool arguments independently of the advertised
// JSON schema: a model or client must not bypass it using unknown/null fields.
func linxCommand(name string, arguments json.RawMessage) (linxchat.Utterance, error) {
	command := linxchat.Utterance{Intent: linxchat.Intent(name)}
	var spec *mcp.Tool
	for _, tool := range linxTools() {
		if tool.Name == name {
			spec = &tool
			break
		}
	}
	if spec == nil {
		return command, fmt.Errorf("未知工具 %q", name)
	}
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(arguments, &fields); err != nil || fields == nil {
		return command, errors.New("工具参数必须是 JSON 对象")
	}
	properties := spec.InputSchema["properties"].(map[string]any)
	for field, value := range fields {
		if _, ok := properties[field]; !ok {
			return command, fmt.Errorf("不支持参数 %q", field)
		}
		if strings.TrimSpace(string(value)) == "null" {
			return command, fmt.Errorf("参数 %q 不能为 null", field)
		}
	}
	for _, field := range []string{"name", "code", "adjustment"} {
		if raw, ok := fields[field]; ok {
			var value string
			if err := json.Unmarshal(raw, &value); err != nil || strings.TrimSpace(value) == "" {
				return command, fmt.Errorf("参数 %q 必须是非空字符串", field)
			}
			switch field {
			case "name":
				value = strings.TrimSpace(value)
				if utf8.RuneCountInString(value) > 32 || strings.ContainsFunc(value, unicode.IsControl) {
					return command, errors.New("名称必须为 1 到 32 字，不能包含控制字符")
				}
				command.Name = value
			case "code":
				if !channel.ValidJoinCode(value) || value != channel.NormalizeJoinCode(value) {
					return command, errors.New("口令必须是六位大写字母或数字，不能包含 0、1、O、I")
				}
				command.Code = value
			case "adjustment":
				if value != "up" && value != "down" {
					return command, errors.New("adjustment 只能是 up 或 down")
				}
				command.Name = "+10"
				if value == "down" {
					command.Name = "-10"
				}
			}
		}
	}
	for _, field := range []string{"index", "level"} {
		if raw, ok := fields[field]; ok {
			var value int
			if err := json.Unmarshal(raw, &value); err != nil {
				return command, fmt.Errorf("参数 %q 必须是整数", field)
			}
			if field == "index" {
				if value < 1 {
					return command, errors.New("频道序号必须从 1 开始")
				}
				command.Index = value
			} else {
				if value < 0 || value > 100 {
					return command, errors.New("音量必须在 0 到 100 之间")
				}
				command.Name = strconv.Itoa(value)
			}
		}
	}
	switch name {
	case "create_channel", "set_nickname":
		if command.Name == "" {
			return command, errors.New("缺少 name，请先询问用户名称")
		}
	case "join_channel", "switch_channel", "set_volume":
		if len(fields) != 1 {
			return command, errors.New("必须且只能提供一个目标参数")
		}
	case "confirm_action":
		command.Intent = linxchat.IntentConfirmJoin
	case "cancel_action":
		command.Intent = linxchat.IntentCancelJoin
	}
	return command, nil
}
