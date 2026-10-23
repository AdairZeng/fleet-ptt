import { useEffect, useState } from 'react'
import { fetchMe, type Me, type Session } from '../state/session.ts'

export function ChannelPage({
  session,
  onChangeNickname,
}: {
  session: Session
  onChangeNickname: () => void
}) {
  const [me, setMe] = useState<Me | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    fetchMe(session)
      .then((next) => {
        if (!cancelled) {
          setMe(next)
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : '身份无效')
        }
      })
    return () => {
      cancelled = true
    }
  }, [session])

  return (
    <main>
      <h1>对讲</h1>
      {me ? (
        <>
          <p>{me.nickname}，已进入。这台设备已绑定到你的账号。</p>
          <p className="meta">device_id {me.device_id}</p>
        </>
      ) : error ? (
        <p className="error">{error}</p>
      ) : (
        <p>正在确认身份…</p>
      )}
      <p className="hint">对讲频道尚未接入。后续语音指令会落到这台设备。</p>
      <button type="button" className="secondary" onClick={onChangeNickname}>
        更换昵称
      </button>
    </main>
  )
}
