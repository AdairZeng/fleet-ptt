import { useState, type FormEvent } from 'react'
import type { JoinLink } from '../state/joinLink.ts'

export function NicknamePage({
  onEnter,
  joinRequest,
}: {
  onEnter: (nickname: string) => Promise<void>
  joinRequest: JoinLink | null
}) {
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
    <main className="nickname-page">
      <p className="kicker">很高兴认识你</p>
      <h1>怎么称呼你？</h1>
      <p>设置一个伙伴们熟悉的昵称，小路会记住你。</p>
      {joinRequest?.ok ? (
        <p className="hint">已扫到入群码 {joinRequest.code}。填写昵称后会加入这个频道。</p>
      ) : null}
      {joinRequest && !joinRequest.ok ? (
        <p className="error" role="alert">
          {joinRequest.message}
        </p>
      ) : null}
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
          {pending ? '进入中…' : '开始同行'}
        </button>
      </form>
    </main>
  )
}
