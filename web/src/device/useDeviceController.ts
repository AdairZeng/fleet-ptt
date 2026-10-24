import { useCallback, useEffect, useReducer, useRef, useState } from 'react'
import type { DeviceAction } from './DeviceShell.tsx'
import { DeviceFeedback } from './feedback.ts'
import {
  initialDeviceSnapshot,
  reduceDevice,
  type DeviceEvent,
} from './controller.ts'
import { readJoinLocation } from '../state/joinLink.ts'
import { loadDeviceCredential, registerDevice } from '../state/device.ts'
import {
  createSession,
  authHeaders,
  loadSession,
  saveSession,
  type Session,
} from '../state/session.ts'

export function useDeviceController() {
  const [session, setSession] = useState<Session | null>(loadSession)
  const [displayNickname, setDisplayNickname] = useState(() => session?.nickname ?? '')
  const [editingNickname, setEditingNickname] = useState(false)
  const [device, dispatchDevice] = useReducer(reduceDevice, initialDeviceSnapshot)
  const [feedback] = useState(() => new DeviceFeedback())
  const [soundError, setSoundError] = useState('')
  const lifecycle = useRef(0)
  const controls = useRef<(action: DeviceAction) => void>(() => {})
  const reportDevice = useCallback((event: DeviceEvent) => dispatchDevice(event), [])
  const [joinRequest] = useState(() => readJoinLocation(window.location))

  async function enter(nickname: string) {
    const revision = lifecycle.current
    const next = await createSession(nickname, session)
    if (revision !== lifecycle.current) throw new Error('设备已关机，设置结果未应用')
    const response = await fetch('/api/v1/device/boot', { method: 'POST', headers: authHeaders(next) })
    if (!response.ok) throw new Error('灵汐设备绑定失败')
    saveSession(next)
    setSession(next)
    setDisplayNickname(next.nickname)
    setEditingNickname(false)
  }

  function changeNickname() {
    controls.current('command-cancel')
    feedback.stop()
    setEditingNickname(true)
  }

  useEffect(() => () => { lifecycle.current += 1; feedback.close() }, [feedback])

  useEffect(() => {
    if (!device.powered) { feedback.close(); return }
    if (device.sound === 'none') return
    void feedback.play(device.sound).catch((error: unknown) => {
      if (error instanceof Error && error.name !== 'AbortError') setSoundError(error.message)
    })
  }, [device.phase, device.powered, device.sound, feedback])

  const unlockSound = () => {
    void feedback.unlock().then(
      () => setSoundError(''),
      (error: unknown) => setSoundError(error instanceof Error ? error.message : '声音不可用'),
    )
  }

  const releaseSoundGesture = () => {
    if (!device.powered) feedback.close()
  }

  useEffect(() => {
    if (device.powered) return
    const close = () => feedback.close()
    window.addEventListener('blur', close)
    return () => window.removeEventListener('blur', close)
  }, [device.powered, feedback])

  useEffect(() => {
    if (device.phase !== 'booting') return
    let cancelled = false
    async function boot() {
      try {
        const credential = loadDeviceCredential() ?? await registerDevice(session?.session_token)
        const headers = session ? authHeaders(session) : new Headers({
          'X-Device-ID': credential.device_id,
          'X-Device-Credential': credential.device_credential,
        })
        const response = await fetch('/api/v1/device/boot', { method: 'POST', headers })
        if (!response.ok) throw new Error('灵汐设备绑定失败')
        await new Promise((resolve) => window.setTimeout(resolve, 250))
        if (!cancelled) dispatchDevice({ type: 'BOOT_READY', hasChannel: false })
      } catch (error) {
        if (!cancelled) dispatchDevice({ type: 'FAIL' })
        setSoundError(error instanceof Error ? error.message : '设备启动失败')
      }
    }
    void boot()
    return () => { cancelled = true }
  }, [device.phase, session])

  const onDeviceAction = (action: DeviceAction) => {
    if (action === 'power') {
      lifecycle.current += 1
      if (device.powered) { controls.current('command-cancel'); feedback.close() }
      dispatchDevice({ type: 'POWER_TOGGLE' })
      return
    }
    if (!device.powered || device.phase === 'booting') return
    if (action === 'command-cancel') feedback.stop()
    controls.current(action)
  }
  const status = `${device.screen} · 小路${session ? ` · ${displayNickname}` : ''}`
  return {
    device,
    session,
    editingNickname,
    joinRequest,
    enter,
    changeNickname,
    controls,
    reportDevice,
    setDisplayNickname,
    onDeviceAction,
    status,
    feedback,
    unlockSound,
    releaseSoundGesture,
    soundError,
  }
}
