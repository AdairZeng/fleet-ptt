package linxchat

// Intent 是结构化业务工具的内部能力名。
type Intent string

const (
	// IntentCreateChannel 对应「建…频道/群」。
	IntentCreateChannel Intent = "create_channel"
	// IntentJoinChannel 对应「加入口令 / 进…」。
	IntentJoinChannel Intent = "join_channel"
	// IntentConfirmJoin 是在待确认口令上说「确认加入」。
	IntentConfirmJoin Intent = "confirm_join"
	// IntentCancelJoin 是取消尚未执行的口令入群。
	IntentCancelJoin Intent = "cancel_join"
	// IntentSwitchChannel 对应「切换到 / 切到」。
	IntentSwitchChannel Intent = "switch_channel"
	// IntentTalkStart 对应「开始对讲」。
	IntentTalkStart Intent = "talk_start"
	// IntentTalkEnd 对应「结束对讲」。
	IntentTalkEnd Intent = "talk_end"
	// IntentListChannels 对应「我有哪些频道」。
	IntentListChannels     Intent = "list_my_channels"
	IntentPreviousChannel  Intent = "previous_channel"
	IntentListSpeakers     Intent = "list_speakers"
	IntentShowJoinCode     Intent = "show_join_code"
	IntentShowJoinQR       Intent = "show_join_qr"
	IntentSetVolume        Intent = "set_volume"
	IntentSetNickname      Intent = "set_nickname"
	IntentListOnline       Intent = "list_online_members"
	IntentInvite           Intent = "invite_member"
	IntentAcceptInvitation Intent = "accept_invitation"
	IntentRejectInvitation Intent = "reject_invitation"
)

// Utterance 是通过 MCP 参数校验后的业务命令。
type Utterance struct {
	Intent        Intent
	Name          string
	Code          string
	Index         int
	RecipientName string
	RecipientID   string
	InvitationID  string
}
