import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { createQiniuRoom, type RtnRoom } from '../rtc/index.ts'
import {
  endTalk,
  observeSelfTalk,
  remoteSpeakers,
  selfTalkAction,
  speakingText,
  startTalk,
  type Member,
  type SignalEvent,
} from '../signaling/index.ts'
import { fetchRoomToken } from '../state/rtn.ts'
import { reduceTalk, type TalkPhase } from '../state/talk.ts'
import type { Session } from '../state/session.ts'

type View = 'joining' | 'ready' | 'error'

export function TalkPanel({
  session,
  channelId,
  members,
  subscribe,
  stopUplinkRef,
  leaveRoomRef,
  commanding,
  onVoiceUplink,
}: {
  session: Session
  channelId: string
  members: Member[] | undefined
  subscribe: (listener: (event: SignalEvent) => void) => () => void
  stopUplinkRef: { current: () => Promise<void> }
  leaveRoomRef: { current: () => Promise<void> }
  commanding: boolean
  onVoiceUplink: () => void
}) {
  const [view, setView] = useState<View>('joining')
  const [talk, setTalk] = useState<TalkPhase>('out')
  const [error, setError] = useState('')
  const [listenHint, setListenHint] = useState('')
  const [attempt, setAttempt] = useState(0)
  const roomRef = useRef<RtnRoom | null>(null)
  const holdRef = useRef(false)
  const keyboardHoldRef = useRef(false)
  const wantRef = useRef(false)
  const suppressRef = useRef(0)
  const generation = useRef(0)
  const viewRef = useRef<View>('joining')
  const sessionRef = useRef(session)
  const channelIDRef = useRef(channelId)
  const tailRef = useRef(Promise.resolve())
  const onSignalRef = useRef<(event: SignalEvent) => void>(() => {})
  const sinkRef = useRef<HTMLDivElement>(null)
  const attemptRef = useRef(attempt)

  function setViewNow(next: View) {
    viewRef.current = next
    setView(next)
  }

  function enqueue(task: () => Promise<void>) {
    const run = tailRef.current.then(task, task)
    tailRef.current = run.then(
      () => undefined,
      () => undefined,
    )
  }

  async function publishHeld(gen: number) {
    const room = roomRef.current
    if (!room || viewRef.current !== 'ready' || !wantRef.current) {
      return
    }
    try {
      const result = await room.startPublish()
      if (generation.current !== gen || result === 'cancelled') {
        await room.stopPublish()
        if (generation.current === gen || !wantRef.current) {
          wantRef.current = false
          setTalk((phase) => reduceTalk(phase, 'TALK_END'))
          try {
            await endTalk(sessionRef.current)
          } catch (err) {
            setError(err instanceof Error ? err.message : '对讲状态更新失败')
          }
        }
        return
      }
    } catch (err) {
      if (generation.current !== gen) {
        return
      }
      wantRef.current = false
      holdRef.current = false
      setTalk((phase) => reduceTalk(phase, 'TALK_END'))
      setError(err instanceof Error ? err.message : '无法打开麦克风')
      try {
        await endTalk(sessionRef.current)
      } catch {
        // 麦克风失败时已经把本地状态收回；服务端结束失败留在下一次结束再试。
      }
    }
  }

  async function localStart(gen: number) {
    try {
      await startTalk(sessionRef.current)
    } catch (err) {
      if (generation.current !== gen) {
        return
      }
      suppressRef.current = Math.max(0, suppressRef.current - 1)
      wantRef.current = false
      holdRef.current = false
      setTalk((phase) => reduceTalk(phase, 'TALK_END'))
      setError(err instanceof Error ? err.message : '对讲状态更新失败')
      return
    }
    if (generation.current !== gen) {
      if (!wantRef.current) {
        try {
          await endTalk(sessionRef.current)
        } catch (err) {
          setError(err instanceof Error ? err.message : '对讲状态更新失败')
        }
        await roomRef.current?.stopPublish()
      }
      return
    }
    await publishHeld(gen)
  }

  function stopLocal(): Promise<void> {
    const gen = ++generation.current
    wantRef.current = false
    setTalk((phase) => reduceTalk(phase, 'TALK_END'))
    const run = tailRef.current.then(async () => {
      if (generation.current !== gen) {
        return
      }
      try {
        await endTalk(sessionRef.current)
      } catch (err) {
        if (generation.current === gen) {
          setError(err instanceof Error ? err.message : '对讲状态更新失败')
        }
      }
      if (generation.current !== gen) {
        return
      }
      await roomRef.current?.stopPublish()
    })
    tailRef.current = run.then(
      () => undefined,
      () => undefined,
    )
    return run.then(
      () => undefined,
      () => undefined,
    )
  }

  function beginFromSignal() {
    const observed = observeSelfTalk(suppressRef.current, 'start')
    suppressRef.current = observed.suppress
    if (!observed.follow) {
      return
    }
    onVoiceUplink()
    const gen = ++generation.current
    wantRef.current = true
    setError('')
    setTalk((phase) => (phase === 'out' ? phase : 'talking'))
    enqueue(async () => {
      if (generation.current !== gen || !wantRef.current) {
        return
      }
      await publishHeld(gen)
    })
  }

  function finishFromSignal() {
    holdRef.current = false
    keyboardHoldRef.current = false
    if (!wantRef.current) {
      setTalk((phase) => reduceTalk(phase, 'TALK_END'))
      void roomRef.current?.stopPublish()
      return
    }
    stopLocal()
  }

  useEffect(() => {
    sessionRef.current = session
    channelIDRef.current = channelId
    stopUplinkRef.current = () => {
      holdRef.current = false
      keyboardHoldRef.current = false
      if (!wantRef.current) {
        return roomRef.current?.stopPublish() ?? Promise.resolve()
      }
      return stopLocal()
    }
    leaveRoomRef.current = () => roomRef.current?.leave() ?? Promise.resolve()
    onSignalRef.current = (event) => {
      const action = selfTalkAction(event, channelIDRef.current, sessionRef.current.user_id)
      if (action === 'start') {
        beginFromSignal()
      } else if (action === 'end') {
        finishFromSignal()
      }
    }
  })

  useEffect(() => {
    return subscribe((event) => onSignalRef.current(event))
  }, [subscribe])

  useEffect(() => {
    const sink = sinkRef.current
    if (!sink) {
      return
    }
    attemptRef.current = attempt
    let cancelled = false
    const room = createQiniuRoom(
      () => {},
      sink,
      (err) => {
        if (cancelled) {
          return
        }
        if (isPlaybackBlocked(err)) {
          setListenHint('浏览器拦截了播放，点一下页面后即可听到')
          return
        }
        setError(err instanceof Error ? err.message : '收听失败')
      },
    )
    roomRef.current = room
    void (async () => {
      try {
        const issued = await fetchRoomToken(session, channelId)
        if (cancelled) {
          return
        }
        await room.join(issued.token)
        if (cancelled) {
          return
        }
        setViewNow('ready')
        setTalk((phase) => (wantRef.current ? 'talking' : reduceTalk(phase, 'JOINED_CHANNEL')))
        if (wantRef.current) {
          const gen = generation.current
          enqueue(async () => {
            if (generation.current !== gen || !wantRef.current) {
              return
            }
            await publishHeld(gen)
          })
        }
      } catch (err) {
        if (cancelled) {
          return
        }
        setViewNow('error')
        setError(err instanceof Error ? err.message : '进房失败')
      }
    })()
    return () => {
      cancelled = true
      const wasTalking = wantRef.current
      wantRef.current = false
      holdRef.current = false
      generation.current += 1
      void room.leave()
      if (wasTalking) {
        void endTalk(sessionRef.current).catch(() => {})
      }
    }
  }, [session, channelId, attempt])

  function press(fromKeyboard = false) {
    if (commanding || viewRef.current !== 'ready' || holdRef.current) {
      return
    }
    const room = roomRef.current
    if (!room) {
      return
    }
    holdRef.current = true
    keyboardHoldRef.current = fromKeyboard
    if (wantRef.current) {
      return
    }
    const gen = ++generation.current
    wantRef.current = true
    suppressRef.current += 1
    setError('')
    setTalk((phase) => reduceTalk(phase, 'TALK_START'))
    enqueue(() => localStart(gen))
  }

  function release() {
    if (!holdRef.current) {
      return
    }
    holdRef.current = false
    keyboardHoldRef.current = false
    stopLocal()
  }

  function end() {
    holdRef.current = false
    keyboardHoldRef.current = false
    stopLocal()
  }

  function unlockAudio() {
    const root = sinkRef.current
    if (!root) {
      return
    }
    for (const el of root.querySelectorAll('audio, video')) {
      if (el instanceof HTMLMediaElement) {
        void el.play().catch(() => {})
      }
    }
    setListenHint('')
  }

  function onPointerDown(event: ReactPointerEvent<HTMLButtonElement>) {
    try {
      event.currentTarget.setPointerCapture(event.pointerId)
    } catch {
      // 指针已经结束时捕获会失败，松手仍由 pointerup / pointercancel 处理。
    }
    press()
  }

  const joined = view === 'ready'
  const talking = talk === 'talking'
  const remote = remoteSpeakers(members, session.user_id)

  return (
    <section className="talk">
      <h2>对讲</h2>
      <p className="talk-status" role="status">
        {statusText(view, talk, commanding)}
      </p>
      {error ? <p className="error">{error}</p> : null}
      <div className="speakers" aria-live="polite">
        {remote.map((member) => (
          <p className="speaking" key={member.user_id}>
            {speakingText(member.display_name)}
          </p>
        ))}
      </div>
      {listenHint ? (
        <p className="hint">
          {listenHint}{' '}
          <button type="button" className="secondary" onClick={unlockAudio}>
            打开声音
          </button>
        </p>
      ) : null}
      <div ref={sinkRef} className="remote-audio" />
      <div className="talk-actions">
        <button
          type="button"
          className={talking ? 'ptt talking' : 'ptt'}
          disabled={!joined || commanding}
          aria-pressed={talking}
          onPointerDown={onPointerDown}
          onPointerUp={release}
          onPointerCancel={release}
          onKeyDown={(event) => {
            if (event.repeat || (event.key !== ' ' && event.key !== 'Enter')) {
              return
            }
            event.preventDefault()
            press(true)
          }}
          onKeyUp={(event) => {
            if (event.key !== ' ' && event.key !== 'Enter') {
              return
            }
            event.preventDefault()
            release()
          }}
          onBlur={() => {
            if (keyboardHoldRef.current) {
              release()
            }
          }}
        >
          {talking ? '松开结束' : '按住说话'}
        </button>
        <button type="button" className="end" disabled={!joined} onClick={end}>
          结束
        </button>
      </div>
      {view === 'error' ? (
        <button
          type="button"
          className="secondary"
          onClick={() => {
            setError('')
            setListenHint('')
            setTalk('out')
            setViewNow('joining')
            setAttempt((n) => n + 1)
          }}
        >
          重试进房
        </button>
      ) : null}
    </section>
  )
}

function statusText(view: View, talk: TalkPhase, commanding: boolean): string {
  if (commanding && view !== 'error') {
    return '指令中'
  }
  if (view === 'joining') {
    return '正在进入频道，只收听…'
  }
  if (view === 'error') {
    return '还没进房'
  }
  if (talk === 'talking') {
    return '正在讲'
  }
  return '正在收听'
}

function isPlaybackBlocked(err: unknown): boolean {
  return err instanceof DOMException && err.name === 'NotAllowedError'
}
