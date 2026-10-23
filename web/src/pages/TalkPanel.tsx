import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { createQiniuRoom, type RtnRoom } from '../rtc/index.ts'
import { fetchRoomToken } from '../state/rtn.ts'
import { reduceTalk, type TalkPhase } from '../state/talk.ts'
import type { Session } from '../state/session.ts'

type View = 'joining' | 'ready' | 'error'

export function TalkPanel({ session, channelId }: { session: Session; channelId: string }) {
  const [view, setView] = useState<View>('joining')
  const [talk, setTalk] = useState<TalkPhase>('out')
  const [error, setError] = useState('')
  const [listenHint, setListenHint] = useState('')
  const [speakers, setSpeakers] = useState<string[]>([])
  const [attempt, setAttempt] = useState(0)
  const roomRef = useRef<RtnRoom | null>(null)
  const holdRef = useRef(false)
  const keyboardHoldRef = useRef(false)
  const sinkRef = useRef<HTMLDivElement>(null)
  const attemptRef = useRef(attempt)

  useEffect(() => {
    const sink = sinkRef.current
    if (!sink) {
      return
    }
    attemptRef.current = attempt
    let cancelled = false
    const room = createQiniuRoom(
      (ids) => {
        if (!cancelled) {
          setSpeakers(ids)
        }
      },
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
        setView('ready')
        setTalk((phase) => reduceTalk(phase, 'JOINED_CHANNEL'))
      } catch (err) {
        if (cancelled) {
          return
        }
        setView('error')
        setError(err instanceof Error ? err.message : '进房失败')
      }
    })()
    return () => {
      cancelled = true
      holdRef.current = false
      void room.leave()
    }
  }, [session, channelId, attempt])

  function press(fromKeyboard = false) {
    if (view !== 'ready' || holdRef.current) {
      return
    }
    const room = roomRef.current
    if (!room) {
      return
    }
    holdRef.current = true
    keyboardHoldRef.current = fromKeyboard
    setError('')
    setTalk((phase) => reduceTalk(phase, 'TALK_START'))
    const generation = attemptRef.current
    void room
      .startPublish()
      .then((result) => {
        if (attemptRef.current !== generation) {
          return
        }
        if (result === 'cancelled') {
          setTalk((phase) => reduceTalk(phase, 'TALK_END'))
        }
      })
      .catch((err: unknown) => {
        if (attemptRef.current !== generation) {
          return
        }
        holdRef.current = false
        setTalk((phase) => reduceTalk(phase, 'TALK_END'))
        setError(err instanceof Error ? err.message : '无法打开麦克风')
      })
  }

  function release() {
    if (!holdRef.current) {
      return
    }
    holdRef.current = false
    keyboardHoldRef.current = false
    setTalk((phase) => reduceTalk(phase, 'TALK_END'))
    void roomRef.current?.stopPublish()
  }

  function end() {
    holdRef.current = false
    keyboardHoldRef.current = false
    setTalk((phase) => reduceTalk(phase, 'TALK_END'))
    void roomRef.current?.stopPublish()
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

  return (
    <section className="talk">
      <h2>对讲</h2>
      <p className="talk-status" role="status">
        {statusText(view, talk)}
      </p>
      {error ? <p className="error">{error}</p> : null}
      {speakers.length > 0 ? <p className="speaking">有人正在说话</p> : null}
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
          disabled={!joined}
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
            setSpeakers([])
            setTalk('out')
            setView('joining')
            setAttempt((n) => n + 1)
          }}
        >
          重试进房
        </button>
      ) : null}
    </section>
  )
}

function statusText(view: View, talk: TalkPhase): string {
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
