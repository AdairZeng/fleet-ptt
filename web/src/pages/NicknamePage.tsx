import { useState, type FormEvent } from 'react'

export function NicknamePage({ onEnter }: { onEnter: (nickname: string) => Promise<void> }) {
  const [nickname, setNickname] = useState('')
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)

  async function submit(event: FormEvent) {
    event.preventDefault()
    const name = nickname.trim()
    if (!name) {
      setError('请输入昵称')
      return
    }
    setPending(true)
    setError('')
    try {
      await onEnter(name)
    } catch (err) {
      setError(err instanceof Error ? err.message : '进入失败')
      setPending(false)
    }
  }

  return (
    <main>
      <h1>fleet-ptt</h1>
      <p>输入昵称进入车队对讲。这台设备会记住身份，下次仍对应到你。</p>
      <form onSubmit={submit}>
        <label>
          昵称
          <input
            name="nickname"
            value={nickname}
            maxLength={32}
            autoComplete="nickname"
            autoFocus
            disabled={pending}
            onChange={(event) => setNickname(event.target.value)}
          />
        </label>
        {error ? <p className="error">{error}</p> : null}
        <button type="submit" disabled={pending}>
          {pending ? '进入中…' : '进入'}
        </button>
      </form>
    </main>
  )
}
