import { CommandGuide } from './CommandGuide.tsx'
import { memberCountText } from '../state/presence.ts'
import { JoinQR } from './JoinQR.tsx'
import { ScanJoin } from './ScanJoin.tsx'
import { TalkPanel } from './TalkPanel.tsx'
import { Invitations } from './Invitations.tsx'
import { useDeviceChannel, type DeviceChannelProps } from '../device/useDeviceChannel.ts'

export function ChannelPage(props: DeviceChannelProps) {
  const { session, onChangeNickname, feedback } = props
  const {
    me,
    identityError,
    channel,
    name,
    setName,
    code,
    setCode,
    createError,
    joinError,
    linkError,
    linkNotice,
    pending,
    signalError,
    channels,
    listError,
    switchError,
    switchNotice,
    voiceNotice,
    wakeMissed,
    voiceReady,
    volumeControl,
    volume,
    voiceStartRef,
    startUplinkRef,
    stopUplinkRef,
    leaveRoomRef,
    finishCommand,
    setRoom,
    onCommand,
    busy,
    arming,
    inCommand,
    presence,
    copy,
    roster,
    hasList,
    inviteRef,
    onCreate,
    onJoin,
    onSwitch,
    enterByCode,
    signaling,
  } = useDeviceChannel(props)
  return (
    <main className="talk-page">
      <header className="talk-bar">
        <div className="talk-bar-title">
          <p className="kicker">{channel ? '当前频道' : '车队对讲'}</p>
          <h1>{channel ? channel.name : '对讲'}</h1>
        </div>
        <div className="talk-bar-meta">
          {channel ? (
            <p className="member-count">{memberCountText(roster?.length)}</p>
          ) : null}
        </div>
      </header>
      {identityError ? <p className="error" role="alert">{identityError}</p> : null}

      <section className={`talk-stage stage-${presence}`} aria-live="polite">
        <p className="talk-glance" role="status">
          {copy.label}
        </p>
        {copy.detail ? <p className="talk-detail">{copy.detail}</p> : null}
      </section>
      <section className="assistant-card" aria-label="小路语音状态">
        <div className="assistant-heading"><strong>小路</strong><span>{voiceReady ? '语音已连接' : '语音未就绪'}</span></div>
        <p role="status">{!voiceReady ? '按指令键重试，并允许麦克风权限' : voiceNotice ? voiceNotice : '说“你好小路”，我在听'}</p>
        {wakeMissed ? <details><summary>最近识别 · 未唤醒</summary><p>“{wakeMissed}”</p></details> : null}
      </section>
      {signalError ? <p className="error" role="alert">{signalError}</p> : null}
      <CommandGuide />
      {linkError ? (
        <p className="error" role="alert">
          {linkError}
        </p>
      ) : null}
      {linkNotice ? (
        <p className="hint" role="status">
          {linkNotice}
        </p>
      ) : null}

      {channel ? (
        <TalkPanel
          feedback={feedback}
          key={channel.channel_id}
          session={session}
          channelId={channel.channel_id}
          members={roster}
          subscribe={signaling.subscribe}
          volumeControl={volumeControl}
          volume={volume}
          voiceStartRef={voiceStartRef}
          startUplinkRef={startUplinkRef}
          stopUplinkRef={stopUplinkRef}
          leaveRoomRef={leaveRoomRef}
          commanding={inCommand}
          sendingDisabled={busy}
          onVoiceUplink={finishCommand}
          onRoomChange={setRoom}
          onCommand={() => void onCommand()}
          commandDisabled={busy || arming}
        />
      ) : null}

      <Invitations session={session} channelName={channel?.name || ''} subscribe={signaling.subscribe} stopUplink={() => stopUplinkRef.current()} />
      <details className="screen-menu">
        <summary>频道管理 <span>切换 · 创建 · 加入 · 分享</span></summary>
        <div className="manual">
        {hasList ? (
          <section className="channel-list" id="channel-list">
            <h2>频道列表</h2>
            <ul>
              {channels?.map((item) => {
                const current = item.channel_id === channel?.channel_id
                return (
                  <li key={item.channel_id}>
                    {current ? (
                      <div className="channel-current">
                        <span>{item.name}</span>
                        <span className="channel-code">{item.join_code}</span>
                        <span className="channel-badge">当前</span>
                      </div>
                    ) : (
                      <button
                        type="button"
                        className="channel-pick"
                        disabled={busy}
                        onClick={() => void onSwitch(item)}
                      >
                        <span>{item.name}</span>
                        <span className="channel-code">{item.join_code}</span>
                      </button>
                    )}
                  </li>
                )
              })}
            </ul>
            {pending === 'switch' ? <p className="hint">正在切换…</p> : null}
            {switchNotice ? (
              <p className="hint" role="status">
                {switchNotice}
              </p>
            ) : null}
            {switchError ? <p className="error">{switchError}</p> : null}
          </section>
        ) : null}
        {listError ? <p className="error">{listError}</p> : null}

        {channel ? (
          <section id="join-code" ref={inviteRef}>
            <h2>口令与二维码</h2>
            <p className="kicker">口令</p>
            <p className="join-code">{channel.join_code}</p>
            <p className="hint">
              {channel.role === 'owner'
                ? '把口令或二维码给其他司机，他们即可加入。'
                : '你已加入这个频道，也可以把二维码给其他司机。'}
            </p>
            <JoinQR
              origin={window.location.origin}
              code={channel.join_code}
              name={channel.name}
            />
          </section>
        ) : null}

        <section id="create-channel">
          <h2>创建频道</h2>
          <form onSubmit={onCreate}>
            <label>
              频道名
              <input
                name="channel-name"
                value={name}
                maxLength={32}
                placeholder="例如 装货"
                autoComplete="off"
                disabled={busy}
                onChange={(event) => setName(event.target.value)}
              />
            </label>
            {createError ? <p className="error">{createError}</p> : null}
            <button type="submit" disabled={busy}>
              {pending === 'create' ? '创建中…' : '创建'}
            </button>
          </form>
        </section>

        <section id="join-by-code">
          <h2>用口令加入</h2>
          <form onSubmit={onJoin}>
            <label>
              6 位口令
              <input
                name="join-code"
                value={code}
                maxLength={32}
                placeholder="例如 3842K9"
                autoComplete="off"
                autoCapitalize="characters"
                spellCheck={false}
                disabled={busy}
                onChange={(event) => setCode(event.target.value)}
              />
            </label>
            {joinError ? <p className="error">{joinError}</p> : null}
            <button type="submit" disabled={busy}>
              {pending === 'join' ? '加入中…' : '加入'}
            </button>
          </form>
          <ScanJoin
            disabled={busy}
            onCode={(nextCode) => void enterByCode(nextCode, 'qr')}
          />
        </section>

        </div>
      </details>
      <details className="screen-menu">
        <summary>设备与隐私 <span>{me?.nickname || session.nickname}</span></summary>
        {me ? <p className="meta">设备 ID：{me.device_id}</p> : null}
        <p className="hint">监听期间麦克风音频会发送至灵汐，关机可停止监听。</p>
        <p className="hint">可说“音量调到五十”或“声音大一点”调整音量。</p>
        <button type="button" className="secondary" onClick={onChangeNickname}>更换昵称</button>
      </details>

      {channel ? null : (
        <div className="talk-dock">
          <div className="talk-dock-inner">
            <button
              type="button"
              className={inCommand ? 'command on solo' : 'command solo'}
              disabled={busy || arming}
              aria-pressed={inCommand}
              onClick={() => void onCommand()}
            >
              {inCommand ? '结束指令' : '指令麦'}
            </button>
          </div>
        </div>
      )}
    </main>
  )
}
