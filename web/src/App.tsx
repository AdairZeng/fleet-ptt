import { useState } from 'react'
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

  if (!session) {
    return <NicknamePage onEnter={enter} joinRequest={joinRequest} />
  }
  return (
    <ChannelPage session={session} onChangeNickname={changeNickname} joinRequest={joinRequest} />
  )
}
