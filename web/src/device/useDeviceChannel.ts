import type { DeviceFeedback } from './feedback.ts'
import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import type { DeviceCommand } from '../signaling/index.ts'
import type { DeviceAction } from './DeviceShell.tsx'
import type { DeviceEvent } from './controller.ts'
import type { RoomSnapshot } from './useDeviceTalk.ts'
import { LinxDevice } from '../linx/index.ts'
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


export type DeviceChannelProps = {
  feedback: DeviceFeedback
  session: Session
  onChangeNickname: () => void
  onNicknameUpdated: (nickname: string) => void
  joinRequest: JoinLink | null
  deviceControls: { current: (action: DeviceAction) => void }
  onDeviceEvent: (event: DeviceEvent) => void
}

export function useDeviceChannel({
  feedback,
  session,
  onNicknameUpdated,
  joinRequest,
  deviceControls,
  onDeviceEvent,
}: DeviceChannelProps) {
  const active = useRef(true)
  useEffect(() => {
    active.current = true
    return () => { active.current = false }
  }, [])

  function requireActive() {
    if (!active.current) throw new Error('设备已关闭')
  }

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
  const openedJoinRef = useRef(false)
  const linxRef = useRef<LinxDevice | null>(null)
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
      if (switchingRef.current) {
        return
      }
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
        requireActive()
        const joined = await joinChannelByCode(
          session,
          source === 'qr' ? normalized : rawCode,
        )
        requireActive()
        if (previousID && previousID !== joined.channel_id) {
          await leaveRoomRef.current()
        }
        requireActive()
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
      if (switchingRef.current) return
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
        if (active.current && next.channel) {
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
  useEffect(() => {
    const device = new LinxDevice(session, {
      onEvent: (event) => {
        if (!active.current) return
        if (event.type === 'stt' && event.text) {
          linxRef.current?.stop()
          setCommanding(false)
          setVoiceNotice(`听到：${event.text}`)
        }
        if (event.type === 'tts' && event.state === 'start') setVoiceNotice('小路正在回答…')
        if (event.type === 'tts' && event.state === 'stop') setVoiceNotice('')
        if (event.type === 'error') setSignalError(event.text || '灵汐连接异常')
      },
    })
    linxRef.current = device
    return () => {
      device.close()
      linxRef.current = null
    }
  }, [session])
  const signaling = useChannelSignaling(session, {
    executeAction: async (action: DeviceCommand, signal: AbortSignal) => {
      requireActive()
      if (switchingRef.current && action.action !== 'talk_end') throw new Error('正在切换频道')
      if (signal.aborted) throw new Error('动作已取消')
      if (action.action === 'talk_end') {
        await stopUplinkRef.current()
      } else if (action.action === 'talk_start') {
        if (!channel || action.channel_id !== channel.channel_id || busy) throw new Error('设备尚未准备好发送')
        finishCommand()
        await voiceStartRef.current(signal)
      } else if (action.action === 'set_volume') {
        const value = action.value === '+10' || action.value === '-10'
          ? Math.min(100, Math.max(0, Math.round(volume * 100) + Number(action.value)))
          : Number(action.value)
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
    if (switchingRef.current) return
    const nextName = name.trim()
    if (!nextName) {
      setCreateError('请输入频道名')
      return
    }
    switchingRef.current = true
    setPending('create')
    setCreateError('')
    try {
      await stopUplinkRef.current()
      requireActive()
      const created = await createChannel(session, nextName)
      requireActive()
      await leaveRoomRef.current()
      requireActive()
      setChannel(created)
      setName('')
      setSwitchNotice('')
      refreshChannels()
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : '创建频道失败')
    } finally {
      switchingRef.current = false
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
    if (switchingRef.current || target.channel_id === channel?.channel_id) {
      return
    }
    setPending('switch')
    setSwitchError('')
    setSwitchNotice('')
    switchingRef.current = true
    try {
      const next = await switchListeningChannel(
        () => stopUplinkRef.current(),
        () => { requireActive(); return switchCurrentChannel(session, target.channel_id) },
        () => { requireActive(); return leaveRoomRef.current() },
      )
      requireActive()
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
    feedback.stop()
    const generation = ++commandGeneration.current
    const next = commandToggle(commanding)
    if (!next.stopUplink) {
      setCommanding(false)
      linxRef.current?.stop()
      return
    }
    setArming(true)
    try {
      await stopUplinkRef.current()
      if (commandGeneration.current !== generation) return
      await linxRef.current?.listen()
      if (commandGeneration.current !== generation) {
        linxRef.current?.stop()
        return
      }
      setCommanding(true)
      setVoiceNotice('请说出指令')
      setSignalError('')
    } catch (err) {
      if (commandGeneration.current === generation) setSignalError(err instanceof Error ? err.message : '停止发送失败')
    } finally {
      if (commandGeneration.current === generation) setArming(false)
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
        feedback.stop()
        commandGeneration.current += 1
        setCommanding(false)
        setArming(false)
        void stopUplinkRef.current().catch(() => {})
        linxRef.current?.stop()
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
    const timer = window.setTimeout(() => {
      linxRef.current?.stop()
      setCommanding(false)
      setVoiceNotice('指令采集超时')
    }, 30000)
    return () => window.clearTimeout(timer)
  }, [commanding])

  const presence = channel
    ? talkPresence(room.view, room.talk, inCommand)
    : idlePresence(inCommand)
  const copy = presenceCopy(presence)
  const roster = channel ? signaling.rosters[channel.channel_id] : undefined
  const hasList = Boolean(channels && channels.length > 0)

  return {
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
    room,
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
  }
}

function toChannel(channel: MeChannel): Channel {
  return {
    channel_id: channel.channel_id,
    name: channel.name,
    join_code: channel.join_code,
    role: channel.role,
  }
}
