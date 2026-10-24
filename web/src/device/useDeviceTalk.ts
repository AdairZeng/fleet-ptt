import type { DeviceFeedback } from './feedback.ts'
import {
  useEffect,
  useRef,
  useState,
} from 'react'
import { createQiniuRoom, type RtnRoom } from '../rtc/index.ts'
import { isPlaybackBlocked } from '../rtc/room.ts'
import {
  endTalk,
  selfTalkAction,
  startTalk,
  type Member,
  type SignalEvent,
} from '../signaling/index.ts'
import { fetchRoomToken } from '../state/rtn.ts'
import type { Session } from '../state/session.ts'
import { reduceTalk, type TalkPhase } from '../state/talk.ts'

type View = 'joining' | 'ready' | 'error'

export type RoomSnapshot = {
  view: View
  talk: TalkPhase
}

export type DeviceTalkProps = {
  feedback: DeviceFeedback
  session: Session
  channelId: string
  members: Member[] | undefined
  subscribe: (listener: (event: SignalEvent) => void) => () => void
  volumeControl: { current: (value: number) => void }
  volume: number
  voiceStartRef: { current: (signal: AbortSignal) => Promise<void> }
  startUplinkRef: { current: () => void }
  stopUplinkRef: { current: () => Promise<void> }
  leaveRoomRef: { current: () => Promise<void> }
  commanding: boolean
  sendingDisabled: boolean
  onVoiceUplink: () => void
  onRoomChange: (room: RoomSnapshot) => void
  onCommand: () => void
  commandDisabled: boolean
}

export function useDeviceTalk({
  feedback,
  session,
  channelId,
  subscribe,
  volumeControl,
  volume,
  voiceStartRef,
  startUplinkRef,
  stopUplinkRef,
  leaveRoomRef,
  commanding,
  sendingDisabled,
  onRoomChange,
}: DeviceTalkProps) {
  const [view, setView] = useState<View>('joining')
  const [talk, setTalk] = useState<TalkPhase>('out')
  const [error, setError] = useState('')
  const [publishing, setPublishing] = useState(false)
  const [listenHint, setListenHint] = useState('')
  const [attempt, setAttempt] = useState(0)
  const roomRef = useRef<RtnRoom | null>(null)
  const holdRef = useRef(false)
  const keyboardHoldRef = useRef(false)
  const wantRef = useRef(false)
  const suppressRef = useRef(0)
  const cue = useRef<AbortController | null>(null)
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
    if (generation.current !== gen || !room || viewRef.current !== 'ready' || !wantRef.current) {
      return
    }
    try {
      setPublishing(true)
      cue.current?.abort()
      const playback = new AbortController()
      cue.current = playback
      await feedback.play('transmit', playback.signal)
      if (generation.current !== gen || !wantRef.current) return
      const result = await room.startPublish()
      if (generation.current !== gen) return
      if (result === 'cancelled') {
        setPublishing(false)
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
      setPublishing(false)
      setTalk((phase) => (phase === 'out' ? phase : 'talking'))
    } catch (err) {
      if (generation.current !== gen) {
        return
      }
      wantRef.current = false
      setPublishing(false)
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
    if (generation.current !== gen || !wantRef.current) return
    try {
      await startTalk(sessionRef.current)
    } catch (err) {
      if (generation.current !== gen) {
        return
      }
      suppressRef.current = Math.max(0, suppressRef.current - 1)
      wantRef.current = false
      holdRef.current = false
      setPublishing(false)
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
    cue.current?.abort()
    setPublishing(false)
    setTalk((phase) => reduceTalk(phase, 'TALK_END'))
    const stopPublish = roomRef.current?.stopPublish() ?? Promise.resolve()
    void stopPublish.catch((err: unknown) => {
      if (generation.current === gen) {
        setError(err instanceof Error ? err.message : '停止发送失败')
      }
    })
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
    })
    tailRef.current = run.then(
      () => undefined,
      () => undefined,
    )
    return stopPublish
  }

  function finishFromSignal() {
    holdRef.current = false
    keyboardHoldRef.current = false
    if (!wantRef.current) {
      setTalk((phase) => reduceTalk(phase, 'TALK_END'))
      void roomRef.current?.stopPublish().catch(() => {})
      return
    }
    stopLocal()
  }

  useEffect(() => {
    volumeControl.current = (value) => { roomRef.current?.setVolume(value) }
    sessionRef.current = session
    channelIDRef.current = channelId
    voiceStartRef.current = async (signal) => {
      if (signal.aborted || viewRef.current !== 'ready' || wantRef.current) throw new Error('设备尚未准备好发送')
      const gen = ++generation.current
      wantRef.current = true
      const cancel = () => {
        if (generation.current === gen) void stopLocal().catch(() => {})
      }
      signal.addEventListener('abort', cancel, { once: true })
      try {
        await publishHeld(gen)
        if (!wantRef.current || generation.current !== gen || signal.aborted) throw new Error('发送失败或已取消')
      } catch (error) {
        signal.removeEventListener('abort', cancel)
        throw error
      }
    }
    startUplinkRef.current = () => press()
    stopUplinkRef.current = () => {
      holdRef.current = false
      keyboardHoldRef.current = false
      if (!wantRef.current) {
        return roomRef.current?.stopPublish() ?? Promise.resolve()
      }
      return stopLocal()
    }
    leaveRoomRef.current = () => {
      const room = roomRef.current
      void stopLocal().catch(() => {})
      setViewNow('joining')
      setTalk('out')
      return room?.leave() ?? Promise.resolve()
    }
    onSignalRef.current = (event) => {
      if (event.type === 'disconnected') {
        holdRef.current = false
        keyboardHoldRef.current = false
        void stopLocal().catch(() => {})
        setViewNow('error')
        setError('连接已断开，发送已停止，请重试进房')
        return
      }
      if (viewRef.current === 'error') return
      const action = selfTalkAction(
        event,
        channelIDRef.current,
        sessionRef.current.user_id,
      )
      if (action === 'start') {
        suppressRef.current = Math.max(0, suppressRef.current - 1)
      } else if (action === 'end') {
        finishFromSignal()
      }
    }
    return () => {
      voiceStartRef.current = async () => { throw new Error('尚未进入频道') }
      startUplinkRef.current = () => {}
      stopUplinkRef.current = async () => {}
      leaveRoomRef.current = async () => {}
      volumeControl.current = () => {}
    }
  })

  useEffect(() => {
    return subscribe((event) => onSignalRef.current(event))
  }, [subscribe])

  useEffect(() => {
    onRoomChange({ view, talk })
  }, [view, talk, onRoomChange])

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
    room.setVolume(volume)
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
        setTalk((phase) => reduceTalk(phase, 'JOINED_CHANNEL'))
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
      if (roomRef.current === room) roomRef.current = null
      const wasTalking = wantRef.current
      wantRef.current = false
      holdRef.current = false
      generation.current += 1
      cue.current?.abort()
      void room.leave().catch(() => {})
      if (wasTalking) {
        void endTalk(sessionRef.current).catch(() => {})
      }
    }
  }, [session, channelId, attempt])

  useEffect(() => {
    const stopIfSending = () => {
      if (holdRef.current || wantRef.current) {
        end()
      }
    }
    const stopWhenHidden = () => {
      if (document.visibilityState === 'hidden') {
        stopIfSending()
      }
    }
    window.addEventListener('blur', stopIfSending)
    document.addEventListener('visibilitychange', stopWhenHidden)
    return () => {
      window.removeEventListener('blur', stopIfSending)
      document.removeEventListener('visibilitychange', stopWhenHidden)
    }
  })

  useEffect(() => { roomRef.current?.setVolume(volume) }, [volume])

  function press(fromKeyboard = false) {
    if (sendingDisabled || commanding || viewRef.current !== 'ready' || holdRef.current) {
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
    setPublishing(true)
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

  async function unlockAudio() {
    const root = sinkRef.current
    if (!root) {
      return
    }
    const media = Array.from(root.querySelectorAll('audio, video')).filter((el): el is HTMLMediaElement => el instanceof HTMLMediaElement)
    if (media.length === 0) return
    try {
      await Promise.all(media.map(el => el.play()))
      setListenHint('')
    } catch {
      setListenHint('声音仍未开启，请保持页面前台并再次点击“打开声音”')
    }
  }

  const joined = view === 'ready'
  const talking = talk === 'talking'

  function retry() {
    setError('')
    setListenHint('')
    setTalk('out')
    setViewNow('joining')
    setAttempt((n) => n + 1)
  }
  return { view, talk, error, publishing, listenHint, sinkRef, joined, talking, press, release, end, unlockAudio, keyboardHoldRef, retry }
}
