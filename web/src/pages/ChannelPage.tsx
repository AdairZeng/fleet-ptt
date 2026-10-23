import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { JoinQR } from './JoinQR.tsx'
import { ScanJoin } from './ScanJoin.tsx'
import { TalkPanel } from './TalkPanel.tsx'
import { useChannelSignaling } from '../signaling/useChannelSignaling.ts'
import {
  createChannel,
  joinChannelByCode,
  listChannels,
  switchCurrentChannel,
  switchListeningChannel,
  type Channel,
} from '../state/channel.ts'
import {
  clearJoinLocation,
  invalidJoinQRMessage,
  isJoinCode,
  normalizeJoinCode,
  type JoinLink,
} from '../state/joinLink.ts'
import { fetchMe, type Me, type MeChannel, type Session } from '../state/session.ts'

export function ChannelPage({
  session,
  onChangeNickname,
  joinRequest,
}: {
  session: Session
  onChangeNickname: () => void
  joinRequest: JoinLink | null
}) {
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
        const joined = await joinChannelByCode(session, source === 'qr' ? normalized : rawCode)
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
  const onChannelCreated = useCallback(
    (event: { channel_id: string; name: string; join_code: string }) => {
      setChannel({
        channel_id: event.channel_id,
        name: event.name,
        join_code: event.join_code,
        role: 'owner',
      })
      refreshChannels()
    },
    [refreshChannels],
  )
  const onChannelSwitched = useCallback(() => {
    refreshChannels()
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
  }, [refreshChannels, session])
  const signaling = useChannelSignaling(session, {
    onChannelCreated,
    onChannelSwitched,
    onError: setSignalError,
  })

  useEffect(() => {
    let cancelled = false
    fetchMe(session)
      .then((next) => {
        if (cancelled) {
          return
        }
        setMe(next)
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
  }, [session])

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

  const busy = pending !== ''

  return (
    <main>
      <h1>对讲</h1>
      {me ? (
        <>
          <p>{me.nickname}，已进入。这台设备已绑定到你的账号。</p>
          <p className="meta">device_id {me.device_id}</p>
        </>
      ) : identityError ? (
        <p className="error">{identityError}</p>
      ) : (
        <p>正在确认身份…</p>
      )}
      {signalError ? <p className="error">{signalError}</p> : null}
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
        <section>
          <h2>当前频道</h2>
          <p className="channel-name">{channel.name}</p>
          <p className="kicker">口令</p>
          <p className="join-code">{channel.join_code}</p>
          <p className="hint">
            {channel.role === 'owner'
              ? '把口令或二维码给其他司机，他们即可加入。'
              : '你已加入这个频道，也可以把二维码给其他司机。'}
          </p>
          <JoinQR origin={window.location.origin} code={channel.join_code} name={channel.name} />
        </section>
      ) : (
        <p className="hint">还没有当前频道。创建一个频道，或输入队长给你的 6 位口令。</p>
      )}

      {channel ? (
        <TalkPanel
          key={channel.channel_id}
          session={session}
          channelId={channel.channel_id}
          members={signaling.rosters[channel.channel_id]}
          subscribe={signaling.subscribe}
          stopUplinkRef={stopUplinkRef}
          leaveRoomRef={leaveRoomRef}
        />
      ) : null}

      {channels && channels.length > 0 ? (
        <section className="channel-list">
          <h2>我的频道</h2>
          <ul>
            {channels.map((item) => {
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

      <section>
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

      <section>
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
        <ScanJoin disabled={busy} onCode={(nextCode) => void enterByCode(nextCode, 'qr')} />
      </section>

      <button type="button" className="secondary" onClick={onChangeNickname}>
        更换昵称
      </button>
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
