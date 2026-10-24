import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { LinxDevice } from '../linx/index.ts'
import { loadDeviceCredential } from '../state/device.ts'
import type { DeviceAction } from '../device/DeviceShell.tsx'
import type { JoinLink } from '../state/joinLink.ts'

export function NicknamePage({
  onEnter,
  joinRequest,
  firstSetup = false,
  registerControls,
}: {
  onEnter: (nickname: string) => Promise<void>
  joinRequest: JoinLink | null
  firstSetup?: boolean
  registerControls?: (handler: (action: DeviceAction) => void) => () => void
}) {
  const [nickname, setNickname] = useState('')
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  const [voiceStatus, setVoiceStatus] = useState('正在连接小路…')
  const [candidate, setCandidate] = useState('')
  const [attempt, setAttempt] = useState(0)
  const confirmedName = useRef('')
  const savingRef = useRef(false)
  const voice = useRef<LinxDevice | null>(null)
  const enterRef = useRef(onEnter)
  const mounted = useRef(true)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  useEffect(() => { enterRef.current = onEnter }, [onEnter])
  const saveNickname = useCallback(async (name: string) => {
    if (savingRef.current) return
    savingRef.current = true
    voice.current?.close()
    setPending(true)
    setError('')
    setVoiceStatus('昵称已确认，正在进入公共频道…')
    try { await enterRef.current(name) } catch (error) {
      if (!mounted.current) return
      setError(error instanceof Error ? error.message : '保存昵称失败')
      setPending(false)
      setVoiceStatus('保存未完成，请重试保存')
    } finally { savingRef.current = false }
  }, [])

  useEffect(() => {
    if (!firstSetup) return
    const credential = loadDeviceCredential()
    if (!credential) return
    let active = true
    const client = new LinxDevice(credential, { onEvent: (event) => {
      if (!active || savingRef.current) return
      if (event.type === 'onboarding') {
        setCandidate(event.nickname ?? '')
        if (event.state === 'speaking') { setError(''); setVoiceStatus(event.text ?? '小路正在说话') }
        if (event.state === 'listening') setVoiceStatus(event.nickname ? '请说“确认”，或“取消”重新设置' : '请说出昵称，例如“叫我老王”')
        if (event.state === 'confirmed' && event.nickname) {
          confirmedName.current = event.nickname
          void saveNickname(event.nickname)
        }
      }
      if (event.type === 'error' || event.type === 'disconnected') setError(event.text ?? '语音设置连接已断开，请重试')
      if (event.type === 'suspended') setVoiceStatus('返回页面后重新进行语音设置')
    } })
    voice.current = client
    const action = (value: DeviceAction) => {
      if (savingRef.current) return
      if (confirmedName.current && value === 'command') { void saveNickname(confirmedName.current); return }
      if (value === 'command') {
        client.unlockAudio()
        void client.listen().catch((error: unknown) => { if (active) setError(error instanceof Error ? error.message : '语音启动失败') })
      }
      if (value === 'command-cancel') client.stop()
    }
    const unregister = registerControls?.(action)
    void client.start().catch((error: unknown) => { if (active) setError(error instanceof Error ? error.message : '语音启动失败') })
    return () => { active = false; client.close(); voice.current = null; unregister?.() }
  }, [firstSetup, registerControls, attempt, saveNickname])

  async function submit(event: FormEvent) {
    event.preventDefault()
    const name = nickname.trim()
    if (!name) {
      setError('请输入昵称')
      return
    }
    confirmedName.current = name
    setCandidate(name)
    await saveNickname(name)
  }

  return (
    <main className="nickname-page">
      <p className="kicker">很高兴认识你</p>
      <h1>怎么称呼你？</h1>
      <p>{firstSetup ? '告诉小路你的昵称，听到复述后再说“确认”。' : '设置一个伙伴们熟悉的昵称，小路会记住你。'}</p>
      {firstSetup ? <section className="assistant-card" aria-label="语音设置">
        <p role="status">{voiceStatus}</p>
        {candidate ? <p>待确认昵称：<strong>{candidate}</strong></p> : null}
        <p className="hint">未听到声音时按指令键开启；保存失败时按指令键重试保存。</p>
      </section> : null}
      {joinRequest?.ok ? (
        <p className="hint">已扫到入群码 {joinRequest.code}。完成昵称设置后会加入这个频道。</p>
      ) : null}
      {joinRequest && !joinRequest.ok ? (
        <p className="error" role="alert">
          {joinRequest.message}
        </p>
      ) : null}
      <details open={!firstSetup}>
        <summary>用文字设置昵称</summary>
        <form onSubmit={submit}>
          <label>
            昵称
            <input
              name="nickname"
              value={nickname}
              maxLength={32}
              autoComplete="nickname"
              autoFocus={!firstSetup}
              disabled={pending}
              onChange={(event) => setNickname(event.target.value)}
            />
          </label>
          <button type="submit" disabled={pending}>
            {pending ? '进入中…' : '开始同行'}
          </button>
        </form>
      </details>
      {error ? <><p className="error" role="alert">{error}</p>{firstSetup ? <button type="button" disabled={pending} onClick={() => { if (confirmedName.current) { void saveNickname(confirmedName.current); return }; setError(''); setCandidate(''); setVoiceStatus('正在连接小路…'); setAttempt((value) => value + 1) }}>{candidate && voiceStatus === '保存未完成，请重试保存' ? '重试保存昵称' : '重试语音设置'}</button> : null}</> : null}
    </main>
  )
}
