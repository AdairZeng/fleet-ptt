# 灵矽接入结论

核对日期：2026-10-23。依据是灵矽公开文档：[第三方应用接入](https://linx.qiniu.com/docs/xrobot/guide/third-app)、[WebSocket 协议](https://linx.qiniu.com/docs/xrobot/platform/websocket)、[OTA](https://linx.qiniu.com/docs/xrobot/platform/OTA)。仓库不存放灵矽密钥，这次没有向灵矽发起在线握手。

## 结论

MVP 使用双页。

- 指令页在灵矽侧：已绑定的设备，或控制台里的设备测试入口。灵矽负责采集、ASR 和 TTS。
- 对讲页是本仓库 H5。七牛 RTN 和 Go WebSocket 信令留在这一页。
- 两边使用同一个 `device_id`。司机在昵称页提交的值必须与回调字段 `auth.device_id` 完全一致，包括大小写和冒号。

本阶段不把 fleet-ptt H5 做成灵矽伪设备。产品分层不变：指令走灵矽，对讲走 RTC。

## 协议里已核对的事实

第三方接入把设备端定义为智能硬件。设备把语音交给灵矽，灵矽再把文字用 Chat SSE 交给业务服务。

设备 WebSocket 握手要带这些 HTTP 头：

- `Authorization: Bearer <token>`
- `Device-Id`：MAC，或由硬件 ID 生成的伪 MAC
- `Client-Id`：UUID
- `Protocol-Version: 1`

浏览器的 `WebSocket` 构造函数不能设置这些头。公开文档没有用查询参数代替这些头的写法，也没有 H5 设备 SDK。

握手之后设备还要发送 `hello`，上行音频是二进制 Opus（16 kHz、常见 60 ms 帧）或 PCM。OTA 注册同样按固件设备设计：`Device-Id` 与请求体 `mac_address` 一致，并带开发板信息。浏览器可以用 `fetch` 调用 OTA，调用之后仍然过不了上述 WebSocket 握手。

因此产品方案 §6.3 的第 1 条（H5 作为伪设备连接 WebSocket）在本仓库的浏览器页面里不成立。第 3 条双页可以落地：指令页用灵矽，对讲页用本应用，靠 `device_id` 对齐。

## 双页怎么对齐

1. 在灵矽控制台把智能体的第三方业务地址设为 `{PUBLIC_BASE_URL}/api/v1/linx/chat`，请求头为 `Authorization: Bearer ${LINX_CHAT_TOKEN}`。
2. 准备一个已绑定该智能体的设备，记下它在回调里使用的 `device_id`。真机或按 OTA 注册的伪 MAC 都可以，注册动作不在浏览器页面里完成。
3. 司机打开本仓库 H5，昵称进入时填写同一个 `device_id`。
4. 在灵矽设备上说指令。灵矽 ASR 之后 `POST` 本服务。本服务校验 token，按 `device_id` 找到用户，并用 SSE 返回 `answer`。灵矽对 `event=message` 且 `type=chat` 的回答做 TTS，在指令页播放。
5. 进房、按住讲话和结束仍在 H5 上操作。H5 上的「指令麦」会先停止上行，再进入指令中；指令本身在灵矽设备上说。语音「开始对讲」由本服务把 `talk_start` 推到该用户的 WebSocket，H5 据此打开上行。

## 本服务已实现的回调

`POST /api/v1/linx/chat`，`Accept: text/event-stream`。

请求体按第三方接入约定读取 `auth.device_id`、`details.msg`、`details.session_id`，`type` 需为 `chat`。

| 情况 | 结果 |
|------|------|
| 未配置 `LINX_CHAT_TOKEN` | 拒绝 |
| `Authorization` 缺失、错误，或误用司机 `session_token` | 拒绝 |
| `device_id` 尚未在 `POST /api/v1/session` 绑定 | 拒绝 |
| 非 `chat`、缺少话术或 `session_id`、JSON 无效 | 拒绝 |
| 校验通过 | `text/event-stream`：一条 `answer`，再一条 `message_end` |

成功时 `session_id` 原样回传。`details.answer` 供 TTS，`details.intent` 是业务归类。语音参数沿用灵矽智能体自己的 TTS 配置，响应里不写死音色。

关键词会执行对应业务，并用 `answer` 做 TTS。除「开始对讲」外，回调会先结束该用户正在进行的上行，再做建群、入群、切群、列举或结束。

| 话术 | intent | 结果 |
|------|--------|------|
| 建一个 / 创建 …频道或群 | `create_channel` | 创建频道。回答含逐位口令，例如「已创建装货。口令是 3、8、4、2、K、9」。WebSocket 推 `channel_created` |
| 加入口令 / 单独报出 6 位口令 | `join_channel` | 第一句只复述口令，不进房。两分钟内再说一次相同口令，或说「确认加入」，才真正加入 |
| 进入 / 进…频道名 | `join_channel` | 按频道名直接加入，不走口令复述 |
| 确认加入 / 确认 / 是的 / 没错 | `confirm_join` | 加入刚才记下的口令。没有待确认口令时说明原因 |
| 取消 / 不对 / 不是 | `cancel_join` | 放弃待确认口令 |
| 切换到 / 切到名称、第 N 个或频道 N | `switch_channel` | 在已加入列表中切换。成功回答「已切换到…」。找不到时回答「未匹配到频道」 |
| 开始对讲 | `talk_start` | 标记正在对讲，推 `talk_start`，H5 打开上行 |
| 结束对讲 | `talk_end` | 结束上行并说明结果 |
| 哪些频道 / 我的频道 | `list_my_channels` | 念出序号、名称和当前频道 |
| 其他 | `echo` | 收到：原文 |

业务失败仍返回 `text/event-stream`，`answer` 说明原因，同时向该用户推 `error`。成功推 `notice`，文案与 `answer` 相同。第一次入群和之后的切群都会推 `channel_switched`，H5 用它进入或换到对应房间。说了别的指令会清掉尚未确认的口令。

环境变量见 [`.env.example`](../.env.example)。真实 token 和公网地址只放本地 `.env`。
