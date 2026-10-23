import { useEffect, useState } from 'react'
import { toDataURL } from 'qrcode'
import { joinQROptions, joinURL } from '../state/joinLink.ts'

export function JoinQR({ origin, code, name }: { origin: string; code: string; name: string }) {
  const url = joinURL(origin, code)
  const [src, setSrc] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    toDataURL(url, joinQROptions)
      .then((next) => {
        if (!cancelled) {
          setSrc(next)
          setError('')
        }
      })
      .catch(() => {
        if (!cancelled) {
          setError('二维码生成失败')
        }
      })
    return () => {
      cancelled = true
    }
  }, [url])

  return (
    <div className="join-qr">
      <p className="kicker">入群二维码</p>
      {src ? <img src={src} width={280} height={280} alt={`${name}的入群二维码`} /> : null}
      {error ? <p className="error">{error}</p> : null}
      <p className="hint">让其他司机用系统相机扫这个码，打开链接即可加入。</p>
      <p className="meta">{url}</p>
    </div>
  )
}
