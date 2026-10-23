import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { TalkPanel } from './TalkPanel.tsx'
import { useChannelSignaling } from '../signaling/useChannelSignaling.ts'
import { createChannel, joinChannelByCode, type Channel } from '../state/channel.ts'
import { fetchMe, type Me, type MeChannel, type Session } from '../state/session.ts'

export function ChannelPage({
  session,
  onChangeNickname,
}: {
  session: Session
  onChangeNickname: () => void
}) {
  const [me, setMe] = useState<Me | null>(null)
  const [identityError, setIdentityError] = useState('')
  const [channel, setChannel] = useState<Channel | null>(null)
  const [name, setName] = useState('')
  const [code, setCode] = useState('')
  const [createError, setCreateError] = useState('')
  const [joinError, setJoinError] = useState('')
  const [pending, setPending] = useState<'create' | 'join' | ''>('')
  const [signalError, setSignalError] = useState('')
  const onChannelCreated = useCallback(
    (event: { channel_id: string; name: string; join_code: string }) => {
      setChannel({
        channel_id: event.channel_id,
        name: event.name,
        join_code: event.join_code,
        role: 'owner',
      })
    },
    [],
  )
  const onChannelSwitched = useCallback(() => {
    fetchMe(session)
      .then((next) => {
        if (next.channel) {
          setChannel(toChannel(next.channel))
        }
      })
      .catch((err: unknown) => {
        setSignalError(err instanceof Error ? err.message : '切换频道失败')
      })
  }, [session])
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
    setPending('join')
    setJoinError('')
    try {
      const joined = await joinChannelByCode(session, nextCode)
      setChannel(joined)
      setCode('')
    } catch (err) {
      setJoinError(err instanceof Error ? err.message : '加入失败')
    } finally {
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

      {channel ? (
        <section>
          <h2>当前频道</h2>
          <p className="channel-name">{channel.name}</p>
          <p className="kicker">口令</p>
          <p className="join-code">{channel.join_code}</p>
          <p className="hint">
            {channel.role === 'owner'
              ? '把口令告诉其他司机，他们输入后即可加入。'
              : '你已用口令加入这个频道。'}
          </p>
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
        />
      ) : null}

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
