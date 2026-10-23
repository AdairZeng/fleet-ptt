import { useEffect, useReducer, useRef, useState } from 'react'
import { DeviceShell, type DeviceAction } from './device/DeviceShell.tsx'
import {
  actionEvent,
  canPerformAction,
  initialDeviceSnapshot,
  phaseLabel,
  reduceDevice,
  syncDeviceResources,
  type DeviceSnapshot,
} from './device/controller.ts'
import { ChannelPage } from './pages/ChannelPage.tsx'
import { NicknamePage } from './pages/NicknamePage.tsx'
import { getOrCreateDeviceID } from './state/device.ts'
import { readJoinLocation } from './state/joinLink.ts'
import {
  clearSession,
  createSession,
  loadSession,
  saveSession,
  type Session,
} from './state/session.ts'

export default function App() {
  const [deviceID] = useState(getOrCreateDeviceID)
  const [session, setSession] = useState<Session | null>(loadSession)
  const [device, dispatchDevice] = useReducer(reduceDevice, initialDeviceSnapshot)
  const previousDevice = useRef<DeviceSnapshot>(device)
  const resources = useRef({
    release: () => {},
    restore: () => {},
  })
  const [joinRequest] = useState(() => readJoinLocation(window.location))

  async function enter(nickname: string) {
    const next = await createSession(nickname, deviceID)
    saveSession(next)
    setSession(next)
  }

  function changeNickname() {
    clearSession()
    setSession(null)
  }

  useEffect(() => {
    syncDeviceResources(previousDevice.current, device, resources.current)
    previousDevice.current = device
  }, [device])

  useEffect(() => {
    if (device.phase !== 'booting') return
    const timer = window.setTimeout(() => {
      dispatchDevice({ type: 'BOOT_READY', hasChannel: Boolean(session) })
    }, 250)
    return () => window.clearTimeout(timer)
  }, [device.phase, session])

  const screen = !session ? (
    <NicknamePage onEnter={enter} joinRequest={joinRequest} />
  ) : (
    <ChannelPage session={session} onChangeNickname={changeNickname} joinRequest={joinRequest} />
  )
  const onDeviceAction = (action: DeviceAction) => {
    if (!canPerformAction(device, action)) {
      return
    }
    dispatchDevice(actionEvent(action, device.phase))
  }
  const status = `${phaseLabel(device.phase)} · 小路${session ? ` · ${session.nickname}` : ''}`
  return <DeviceShell status={status} phase={device.phase} onAction={onDeviceAction}>{screen}</DeviceShell>
}
