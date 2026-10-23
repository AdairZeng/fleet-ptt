import { useCallback, useEffect, useReducer, useRef, useState } from 'react'
import { DeviceShell, type DeviceAction } from './device/DeviceShell.tsx'
import {
  initialDeviceSnapshot,
  reduceDevice,
  type DeviceEvent,
} from './device/controller.ts'
import { ChannelPage } from './pages/ChannelPage.tsx'
import { NicknamePage } from './pages/NicknamePage.tsx'
import { readJoinLocation } from './state/joinLink.ts'
import {
  createSession,
  loadSession,
  saveSession,
  type Session,
} from './state/session.ts'

export default function App() {
  const [session, setSession] = useState<Session | null>(loadSession)
  const [editingNickname, setEditingNickname] = useState(false)
  const [device, dispatchDevice] = useReducer(reduceDevice, initialDeviceSnapshot)
  const controls = useRef<(action: DeviceAction) => void>(() => {})
  const reportDevice = useCallback((event: DeviceEvent) => dispatchDevice(event), [])
  const [joinRequest] = useState(() => readJoinLocation(window.location))

  async function enter(nickname: string) {
    const next = await createSession(nickname, session)
    saveSession(next)
    setSession(next)
    setEditingNickname(false)
  }

  function changeNickname() {
    setEditingNickname(true)
  }

  useEffect(() => {
    if (device.phase !== 'booting') return
    const timer = window.setTimeout(() => {
      dispatchDevice({ type: 'BOOT_READY', hasChannel: false })
    }, 250)
    return () => window.clearTimeout(timer)
  }, [device.phase])

  const screen = !device.powered ? <p>已关机</p> : device.phase === 'booting' ? <p>启动中…</p> : !session || editingNickname ? (
    <NicknamePage onEnter={enter} joinRequest={joinRequest} />
  ) : (
    <ChannelPage session={session} onChangeNickname={changeNickname} joinRequest={joinRequest} deviceControls={controls} onDeviceEvent={reportDevice} />
  )
  const onDeviceAction = (action: DeviceAction) => {
    if (action === 'power') {
      if (device.powered) controls.current('ptt-end')
      dispatchDevice({ type: 'POWER_TOGGLE' })
      return
    }
    if (!device.powered || device.phase === 'booting') return
    controls.current(action)
  }
  const status = `${device.screen} · 小路${session ? ` · ${session.nickname}` : ''}`
  return <DeviceShell status={status} phase={device.phase} onAction={onDeviceAction}>{screen}</DeviceShell>
}
