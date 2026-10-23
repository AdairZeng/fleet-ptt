import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import type { DeviceCommand } from '../signaling/index.ts'
import type { DeviceAction } from '../device/DeviceShell.tsx'
import type { DeviceEvent } from '../device/controller.ts'
import { JoinQR } from './JoinQR.tsx'
import { ScanJoin } from './ScanJoin.tsx'
import { TalkPanel, type RoomSnapshot } from './TalkPanel.tsx'
import { useChannelSignaling } from '../signaling/useChannelSignaling.ts'
import {
  createChannel,
  joinChannelByCode,
  listChannels,
  switchCurrentChannel,
  switchListeningChannel,
  type Channel,
} from '../state/channel.ts'
import { commandToggle } from '../state/command.ts'
import {
  idlePresence,
  memberCountText,
  presenceCopy,
  talkPresence,
} from '../state/presence.ts'
import {
  clearJoinLocation,
  invalidJoinQRMessage,
  isJoinCode,
  normalizeJoinCode,
  type JoinLink,
} from '../state/joinLink.ts'
import {
  fetchMe,
  type Me,
  type MeChannel,
  type Session,
  saveSession,
} from '../state/session.ts'

export function ChannelPage({
  session,
  onChangeNickname,
  onNicknameUpdated,
  joinRequest,
  deviceControls,
  onDeviceEvent,
}: {
  session: Session
  onChangeNickname: () => void
  onNicknameUpdated: (nickname: string) => void
  joinRequest: JoinLink | null
  deviceControls: { current: (action: DeviceAction) => void }
  onDeviceEvent: (event: DeviceEvent) => void
}) {
  const [volume, setVolume] = useState(1)
  const inviteRef = useRef<HTMLElement>(null)
  const [me, setMe] = useState<Me | null>(null)
  const [identityError, setIdentityError] = useState('')
  const [channel, setChannel] = useState<Channel | null>(null)
  const [name, setName] = useState('')
  const [code, setCode] = useState('')
  const [createError, setCreateError] = useState('')
  const [joinError, setJoinError] = useState('')
  const [linkError, setLinkError] = useState(() =>
    joinRequest && !joinRequest.ok ? joinRequest.message : '',
  )
  const [linkNotice, setLinkNotice] = useState('')
  const [pending, setPending] = useState<'create' | 'join' | 'switch' | ''>('')
  const [signalError, setSignalError] = useState('')
  const [channels, setChannels] = useState<Channel[] | null>(null)
  const [listError, setListError] = useState('')
  const [listRevision, setListRevision] = useState(0)
  const [switchError, setSwitchError] = useState('')
  const [switchNotice, setSwitchNotice] = useState('')
  const [voiceNotice, setVoiceNotice] = useState('')
  const [commanding, setCommanding] = useState(false)
  const [arming, setArming] = useState(false)
  const [room, setRoom] = useState<RoomSnapshot>({
    view: 'joining',
    talk: 'out',
  })
  const [roomChannelID, setRoomChannelID] = useState('')
  const volumeControl = useRef<(value: number) => void>(() => {})
  const voiceStartRef = useRef<(signal: AbortSignal) => Promise<void>>(async () => { throw new Error('尚未进入频道') })
  const startUplinkRef = useRef<() => void>(() => {})
  const latestRoom = useRef({ channelID: '', view: 'joining' })
  const commandGeneration = useRef(0)
  const stopUplinkRef = useRef<() => Promise<void>>(async () => {})
  const leaveRoomRef = useRef<() => Promise<void>>(async () => {})
  const switchingRef = useRef(false)
  const joiningRef = useRef(false)
  const openedJoinRef = useRef(false)
  const refreshChannels = useCallback(() => {
    setListRevision((n) => n + 1)
  }, [])
  const enterByCode = useCallback(
    async (rawCode: string, source: 'code' | 'qr') => {
      const normalized = normalizeJoinCode(rawCode)
      if (source === 'qr' && !isJoinCode(normalized)) {
        setLinkError(invalidJoinQRMessage)
        setLinkNotice('')
        return
      }
      if (isJoinCode(normalized) && channel?.join_code === normalized) {
        setJoinError('')
        setLinkError('')
        setLinkNotice('已在这个频道')
        clearJoinLocation()
        return
      }
      if (joiningRef.current) {
        return
      }
      joiningRef.current = true
      setPending('join')
      setJoinError('')
      setLinkError('')
      setLinkNotice('')
      switchingRef.current = true
      const previousID = channel?.channel_id ?? ''
      try {
        if (previousID) {
          await stopUplinkRef.current()
        }
        const joined = await joinChannelByCode(
          session,
          source === 'qr' ? normalized : rawCode,
        )
        if (previousID && previousID !== joined.channel_id) {
          await leaveRoomRef.current()
        }
        setChannel(joined)
        setCode('')
        setLinkNotice(source === 'qr' ? `已加入${joined.name}` : '')
        setSwitchNotice('')
        refreshChannels()
        clearJoinLocation()
      } catch (err) {
        const message = err instanceof Error ? err.message : '加入失败'
        if (source === 'qr') {
          setLinkError(message)
        } else {
          setJoinError(message)
        }
      } finally {
        joiningRef.current = false
        switchingRef.current = false
        setPending('')
      }
    },
    [channel, refreshChannels, session],
  )
  const finishCommand = useCallback(() => {
    setCommanding(false)
  }, [])
  const onChannelCreated = useCallback(
    (event: { channel_id: string; name: string; join_code: string }) => {
      setChannel({
        channel_id: event.channel_id,
        name: event.name,
        join_code: event.join_code,
        role: 'owner',
      })
      finishCommand()
      refreshChannels()
    },
    [finishCommand, refreshChannels],
  )
  const onChannelSwitched = useCallback(() => {
    refreshChannels()
    finishCommand()
    if (switchingRef.current) {
      return
    }
    fetchMe(session)
      .then((next) => {
        if (next.channel) {
          setChannel(toChannel(next.channel))
        }
      })
      .catch((err: unknown) => {
        setSignalError(err instanceof Error ? err.message : '切换频道失败')
      })
  }, [finishCommand, refreshChannels, session])
  const onSignalError = useCallback(
    (message: string) => {
      setSignalError(message)
      setVoiceNotice('')
      finishCommand()
    },
    [finishCommand],
  )
  const onNotice = useCallback(
    (message: string) => {
      setVoiceNotice(message)
      setSignalError('')
      finishCommand()
    },
    [finishCommand],
  )
  const signaling = useChannelSignaling(session, {
    executeAction: async (action: DeviceCommand, signal: AbortSignal) => {
      if (signal.aborted) throw new Error('动作已取消')
      if (action.action === 'talk_end') {
        await stopUplinkRef.current()
      } else if (action.action === 'talk_start') {
        if (!channel || action.channel_id !== channel.channel_id || busy) throw new Error('设备尚未准备好发送')
        finishCommand()
        await voiceStartRef.current(signal)
      } else if (action.action === 'set_volume') {
        const value = Number(action.value)
        if (!action.value || !Number.isFinite(value) || value < 0 || value > 100) throw new Error('音量无效')
        volumeControl.current(value / 100)
        setVolume(value / 100)
      } else if (action.action === 'show_join_qr') {
        if (!inviteRef.current || action.channel_id !== channel?.channel_id) throw new Error('当前频道二维码未就绪')
        const qr = inviteRef.current.querySelector('img')
        if (!qr?.complete || qr.naturalWidth === 0) throw new Error('二维码尚未生成')
        inviteRef.current.scrollIntoView({ block: 'center' })
      } else if (action.action === 'set_nickname') {
        const profile = await fetchMe(session)
        if (signal.aborted || profile.nickname !== action.value) throw new Error('昵称同步失败')
        setMe(profile)
        onNicknameUpdated(profile.nickname)
        saveSession({ ...session, nickname: profile.nickname })
      } else if (action.action === 'switch_channel') {
        await stopUplinkRef.current()
        if (signal.aborted) throw new Error('动作已取消')
        const next = await fetchMe(session)
        if (!next.channel || next.channel.channel_id !== action.channel_id) throw new Error('目标频道不匹配')
        if (channel?.channel_id !== next.channel.channel_id) {
          await leaveRoomRef.current()
          if (signal.aborted) throw new Error('动作已取消')
          setChannel(toChannel(next.channel))
        }
        await new Promise<void>((resolve, reject) => {
          const start = Date.now()
          const check = () => {
            if (signal.aborted) { reject(new Error('动作已取消')); return }
            if (latestRoom.current.channelID === action.channel_id && latestRoom.current.view === 'ready') { resolve(); return }
            if (Date.now() - start > 12000) { reject(new Error('进房超时')); return }
            window.setTimeout(check, 50)
          }
          check()
        })
      } else {
        throw new Error('尚不支持该设备动作')
      }
    },
    onChannelCreated,
    onChannelSwitched,
    onError: onSignalError,
    onNotice,
  })

  useEffect(() => {
    let cancelled = false
    fetchMe(session)
      .then((next) => {
        if (cancelled) {
          return
        }
        setMe(next)
        onNicknameUpdated(next.nickname)
        const restored = next.channel
        if (restored) {
          setChannel((current) => current ?? toChannel(restored))
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setIdentityError(err instanceof Error ? err.message : '身份无效')
        }
      })
    return () => {
      cancelled = true
    }
  }, [session, onNicknameUpdated])

  useEffect(() => {
    if (openedJoinRef.current || !joinRequest?.ok) {
      return
    }
    openedJoinRef.current = true
    void enterByCode(joinRequest.code, 'qr')
  }, [enterByCode, joinRequest])

  useEffect(() => {
    let cancelled = false
    listChannels(session)
      .then((next) => {
        if (cancelled) {
          return
        }
        setChannels(next)
        setListError('')
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setListError(err instanceof Error ? err.message : '读取频道列表失败')
        }
      })
    return () => {
      cancelled = true
    }
  }, [session, listRevision])

  async function onCreate(event: FormEvent) {
    event.preventDefault()
    const nextName = name.trim()
    if (!nextName) {
      setCreateError('请输入频道名')
      return
    }
    setPending('create')
    setCreateError('')
    try {
      const created = await createChannel(session, nextName)
      setChannel(created)
      setName('')
      setSwitchNotice('')
      refreshChannels()
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : '创建频道失败')
    } finally {
      setPending('')
    }
  }

  async function onJoin(event: FormEvent) {
    event.preventDefault()
    const nextCode = code.trim()
    if (!nextCode) {
      setJoinError('请输入口令')
      return
    }
    await enterByCode(nextCode, 'code')
  }

  async function onSwitch(target: Channel) {
    if (pending !== '' || target.channel_id === channel?.channel_id) {
      return
    }
    setPending('switch')
    setSwitchError('')
    setSwitchNotice('')
    switchingRef.current = true
    try {
      const next = await switchListeningChannel(
        () => stopUplinkRef.current(),
        () => switchCurrentChannel(session, target.channel_id),
        () => leaveRoomRef.current(),
      )
      setChannel(next)
      setSwitchNotice(`已切换到${next.name}`)
      refreshChannels()
    } catch (err) {
      setSwitchError(err instanceof Error ? err.message : '切换频道失败')
    } finally {
      switchingRef.current = false
      setPending('')
    }
  }

  async function onCommand() {
    const generation = ++commandGeneration.current
    const next = commandToggle(commanding)
    if (!next.stopUplink) {
      setCommanding(false)
      return
    }
    setArming(true)
    try {
      await stopUplinkRef.current()
      if (commandGeneration.current !== generation) return
      setCommanding(false)
      setVoiceNotice('语音暂未就绪，请使用屏幕操作。')
      setSignalError('')
    } catch (err) {
      setSignalError(err instanceof Error ? err.message : '停止发送失败')
    } finally {
      setArming(false)
    }
  }

  const busy = pending !== ''
  const inCommand = commanding || arming
  const activeChannelID = channel?.channel_id ?? ''
  if (roomChannelID !== activeChannelID) {
    setRoomChannelID(activeChannelID)
    setRoom({ view: 'joining', talk: 'out' })
  }
  useEffect(() => {
    latestRoom.current = { channelID: activeChannelID, view: room.view }
  }, [activeChannelID, room.view])

  useEffect(() => {
    if (inCommand) onDeviceEvent({ type: 'COMMAND_START' })
    else onDeviceEvent({ type: 'MEDIA_STATE', ready: Boolean(channel) && room.view === 'ready', talking: room.talk === 'talking', failed: Boolean(channel) && room.view === 'error' })
  }, [channel, inCommand, room, onDeviceEvent])

  useEffect(() => {
    deviceControls.current = (action) => {
      if (action === 'command-cancel') {
        commandGeneration.current += 1
        setCommanding(false)
        setArming(false)
        void stopUplinkRef.current().catch(() => {})
      } else if (action === 'ptt-end') {
        void stopUplinkRef.current().catch(() => {})
      } else if (action === 'ptt-start' && !inCommand && !busy) {
        startUplinkRef.current()
      } else if (action === 'command' && !arming && !busy) {
        void onCommand()
      }
    }
    return () => { deviceControls.current = () => {} }
  })

  useEffect(() => () => { commandGeneration.current += 1 }, [])

  useEffect(() => {
    if (!commanding) return
    const timer = window.setTimeout(() => setCommanding(false), 30000)
    return () => window.clearTimeout(timer)
  }, [commanding])

  const presence = channel
    ? talkPresence(room.view, room.talk, inCommand)
    : idlePresence(inCommand)
  const copy = presenceCopy(presence)
  const roster = channel ? signaling.rosters[channel.channel_id] : undefined
  const hasList = Boolean(channels && channels.length > 0)

  return (
    <main className="talk-page">
      <header className="talk-bar">
        <div className="talk-bar-title">
          <p className="kicker">{channel ? '当前频道' : '车队对讲'}</p>
          <h1>{channel ? channel.name : '对讲'}</h1>
        </div>
        <div className="talk-bar-meta">
          <p className={`presence presence-${presence}`} aria-hidden="true">
            {copy.label}
          </p>
          {channel ? (
            <p className="member-count">{memberCountText(roster?.length)}</p>
          ) : null}
        </div>
      </header>
      {me ? (
        <p className="meta">
          {me.nickname} 已绑定 device_id {me.device_id}
        </p>
      ) : identityError ? (
        <p className="error">{identityError}</p>
      ) : (
        <p className="hint">正在确认身份…</p>
      )}

      <section className={`talk-stage stage-${presence}`} aria-live="polite">
        <p className="talk-glance" role="status">
          {copy.label}
        </p>
        {copy.detail ? <p className="talk-detail">{copy.detail}</p> : null}
      </section>
      {inCommand && presence !== 'commanding' ? (
        <p className="hint">发送已停止。语音暂未就绪，请使用屏幕操作。</p>
      ) : null}
      {signalError ? <p className="error">{signalError}</p> : null}
      {voiceNotice ? (
        <p className="hint" role="status">
          {voiceNotice}
        </p>
      ) : null}
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
          onVoiceUplink={finishCommand}
          onRoomChange={setRoom}
          onCommand={() => void onCommand()}
          commandDisabled={busy || arming}
        />
      ) : null}

      <nav className="manual-jumps" aria-label="手动兜底">
        {hasList ? <a href="#channel-list">频道列表</a> : null}
        {channel ? <a href="#join-code">口令二维码</a> : null}
        <a href="#join-by-code">口令 / 扫码</a>
        <a href="#create-channel">创建频道</a>
      </nav>

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

        <button type="button" className="secondary" onClick={onChangeNickname}>
          更换昵称
        </button>
      </div>

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

function toChannel(channel: MeChannel): Channel {
  return {
    channel_id: channel.channel_id,
    name: channel.name,
    join_code: channel.join_code,
    role: channel.role,
  }
}
