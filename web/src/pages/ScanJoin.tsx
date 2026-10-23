import { useEffect, useRef, useState } from 'react'
import { invalidJoinQRMessage, readJoinPayload } from '../state/joinLink.ts'

type DetectedBarcode = { rawValue?: string }

type BarcodeDetectorInstance = {
  detect: (source: HTMLVideoElement) => Promise<DetectedBarcode[]>
}

function createBarcodeDetector(): BarcodeDetectorInstance | null {
  const host = globalThis as typeof globalThis & {
    BarcodeDetector?: new (options: { formats: string[] }) => BarcodeDetectorInstance
  }
  if (!host.BarcodeDetector) {
    return null
  }
  return new host.BarcodeDetector({ formats: ['qr_code'] })
}

export function ScanJoin({
  disabled,
  onCode,
}: {
  disabled: boolean
  onCode: (code: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [error, setError] = useState('')
  const videoRef = useRef<HTMLVideoElement>(null)
  const onCodeRef = useRef(onCode)

  useEffect(() => {
    onCodeRef.current = onCode
  }, [onCode])

  useEffect(() => {
    if (!open) {
      return
    }
    const video = videoRef.current
    const detector = createBarcodeDetector()
    if (!video || !detector) {
      return
    }
    const handle = { stopped: false, stream: null as MediaStream | null, timer: 0 }

    async function run(preview: HTMLVideoElement, scanner: BarcodeDetectorInstance) {
      try {
        handle.stream = await navigator.mediaDevices.getUserMedia({
          audio: false,
          video: { facingMode: 'environment' },
        })
      } catch {
        if (!handle.stopped) {
          setError('无法打开相机，请改用系统相机扫二维码')
        }
        return
      }
      if (handle.stopped) {
        stopStream(handle.stream)
        return
      }
      preview.srcObject = handle.stream
      try {
        await preview.play()
      } catch {
        if (!handle.stopped) {
          setError('无法打开相机，请改用系统相机扫二维码')
        }
        return
      }
      let last = ''
      const tick = async () => {
        if (handle.stopped) {
          return
        }
        try {
          const codes = await scanner.detect(preview)
          const raw = codes[0]?.rawValue?.trim() ?? ''
          if (raw && raw !== last) {
            last = raw
            const parsed = readJoinPayload(raw)
            if (!parsed || !parsed.ok) {
              setError(parsed?.message ?? invalidJoinQRMessage)
            } else {
              handle.stopped = true
              setOpen(false)
              onCodeRef.current(parsed.code)
              return
            }
          }
        } catch {
          // 某一帧识别失败就等下一帧。
        }
        if (!handle.stopped) {
          handle.timer = window.setTimeout(() => void tick(), 250)
        }
      }
      void tick()
    }

    void run(video, detector)
    return () => {
      handle.stopped = true
      window.clearTimeout(handle.timer)
      stopStream(handle.stream)
      video.srcObject = null
    }
  }, [open])

  return (
    <div className="scan-join">
      <button
        type="button"
        className="secondary"
        disabled={disabled || open}
        onClick={() => {
          setError('')
          if (!createBarcodeDetector()) {
            setError('这个浏览器不能在页面里扫码，请用系统相机扫二维码')
            return
          }
          setOpen(true)
        }}
      >
        打开相机扫码
      </button>
      {open ? <video ref={videoRef} muted playsInline autoPlay /> : null}
      {open ? (
        <button type="button" className="secondary" onClick={() => setOpen(false)}>
          关闭相机
        </button>
      ) : null}
      {error ? <p className="error">{error}</p> : null}
    </div>
  )
}

function stopStream(stream: MediaStream | null) {
  stream?.getTracks().forEach((track) => track.stop())
}
