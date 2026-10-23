import { useState } from 'react'
import { DeviceShell, type DeviceAction } from './device/DeviceShell.tsx'
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

  const screen = !session ? (
    <NicknamePage onEnter={enter} joinRequest={joinRequest} />
  ) : (
    <ChannelPage session={session} onChangeNickname={changeNickname} joinRequest={joinRequest} />
  )
  const onDeviceAction = (action: DeviceAction) => {
    if (action === 'power') {
      return
    }
    // 3D controls expose semantic actions; the device controller will own execution.
  }
  return <DeviceShell status={session ? '在线 · 小路' : '待设置 · 小路'} onAction={onDeviceAction}>{screen}</DeviceShell>
}
