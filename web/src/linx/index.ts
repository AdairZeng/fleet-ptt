import type { Session } from '../state/session.ts'

type LinxEvent = {
  type: string
  state?: string
  text?: string
  session_id?: string
  is_aborted?: boolean
}

export type LinxHandlers = {
  onEvent?: (event: LinxEvent) => void
  onBinary?: (payload: ArrayBuffer) => void
}

// LinxDevice is the browser side of the real device protocol. The Go endpoint
// keeps the Linx token private and forwards the WebSocket frames unchanged.
export class LinxDevice {
  private readonly session: Session
  private readonly handlers: LinxHandlers
  private socket: WebSocket | null = null
  private audioContext: AudioContext | null = null
  private source: MediaStreamAudioSourceNode | null = null
  private processor: ScriptProcessorNode | null = null
  private stream: MediaStream | null = null
  private sessionID = ''
  private sampleRate = 16000
  private helloReceived = false
  private pendingPCM = new Uint8Array(0)
  private decoder: AudioDecoder | null = null
  private playbackTime = 0

  constructor(session: Session, handlers: LinxHandlers = {}) {
    this.session = session
    this.handlers = handlers
  }

  async connect(): Promise<void> {
    if (this.socket?.readyState === WebSocket.OPEN) return
    const protocol = location.protocol === 'https:' ? 'wss' : 'ws'
    const socket = new WebSocket(`${protocol}://${location.host}/api/v1/linx/ws?token=${encodeURIComponent(this.session.session_token)}`)
    socket.binaryType = 'arraybuffer'
    this.socket = socket
    await new Promise<void>((resolve, reject) => {
      socket.onopen = () => resolve()
      socket.onerror = () => reject(new Error('灵汐连接失败'))
      socket.onclose = () => this.handlers.onEvent?.({ type: 'disconnected' })
      socket.onmessage = (event) => {
        if (typeof event.data === 'string') {
          try {
            const message = JSON.parse(event.data) as LinxEvent & { audio_params?: { sample_rate?: number } }
            if (message.type === 'hello') {
              this.helloReceived = true
              if (message.audio_params?.sample_rate) this.sampleRate = message.audio_params.sample_rate
            }
            if (message.session_id) this.sessionID = message.session_id
            this.handlers.onEvent?.(message)
          } catch {
            this.handlers.onEvent?.({ type: 'error', text: '灵汐消息格式无效' })
          }
        } else if (event.data instanceof ArrayBuffer) {
          this.playTTS(event.data)
          this.handlers.onBinary?.(event.data)
        }
      }
    })
    await this.waitForHello()
  }

  async listen(): Promise<void> {
    await this.connect()
    await this.startMicrophone()
    this.send({ session_id: this.sessionID, type: 'listen', state: 'start', mode: 'auto' })
  }

  stop(): void {
    if (this.socket?.readyState === WebSocket.OPEN) this.send({ session_id: this.sessionID, type: 'listen', state: 'stop' })
    this.processor?.disconnect()
    this.source?.disconnect()
    this.stream?.getTracks().forEach((track) => track.stop())
    this.processor = null
    this.source = null
    this.stream = null
  }

  close(): void {
    this.stop()
    this.socket?.close()
    void this.audioContext?.close()
    this.socket = null
    this.audioContext = null
    this.helloReceived = false
    this.pendingPCM = new Uint8Array(0)
    this.decoder?.close()
    this.decoder = null
    this.playbackTime = 0
  }

  private send(message: object): void {
    if (this.socket?.readyState !== WebSocket.OPEN) throw new Error('灵汐连接未就绪')
    this.socket.send(JSON.stringify(message))
  }

  private async waitForHello(): Promise<void> {
    if (this.helloReceived) return
    await new Promise<void>((resolve, reject) => {
      const timer = window.setTimeout(() => reject(new Error('灵汐握手超时')), 10000)
      const check = () => {
        if (this.helloReceived) {
          window.clearTimeout(timer)
          resolve()
        } else window.setTimeout(check, 25)
      }
      check()
    })
  }

  private async startMicrophone(): Promise<void> {
    if (this.processor) return
    this.stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true } })
    this.audioContext = new AudioContext()
    await this.audioContext.resume()
    this.source = this.audioContext.createMediaStreamSource(this.stream)
    this.processor = this.audioContext.createScriptProcessor(2048, 1, 1)
    this.processor.onaudioprocess = (event) => {
      if (this.socket?.readyState !== WebSocket.OPEN) return
      const input = event.inputBuffer.getChannelData(0)
      const pcm = new Uint8Array(downsample(input, this.audioContext?.sampleRate ?? 48000, this.sampleRate))
      if (pcm.byteLength > 0) {
        const merged = new Uint8Array(this.pendingPCM.byteLength + pcm.byteLength)
        merged.set(this.pendingPCM)
        merged.set(pcm, this.pendingPCM.byteLength)
        let offset = 0
        while (merged.byteLength - offset >= 640) {
          this.socket.send(merged.slice(offset, offset + 640))
          offset += 640
        }
        this.pendingPCM = merged.slice(offset)
      }
    }
    this.source.connect(this.processor)
    this.processor.connect(this.audioContext.destination)
  }

  private playTTS(payload: ArrayBuffer): void {
    if (typeof AudioDecoder === 'undefined' || typeof EncodedAudioChunk === 'undefined') return
    this.audioContext ??= new AudioContext()
    if (!this.decoder) {
      this.decoder = new AudioDecoder({
        output: (audio) => {
          const context = this.audioContext
          if (!context) return
          const buffer = context.createBuffer(audio.numberOfChannels, audio.numberOfFrames, audio.sampleRate)
          for (let channel = 0; channel < audio.numberOfChannels; channel += 1) {
            const samples = new Float32Array(audio.numberOfFrames)
            audio.copyTo(samples, { planeIndex: channel })
            buffer.copyToChannel(samples, channel)
          }
          audio.close()
          const source = context.createBufferSource()
          source.buffer = buffer
          source.connect(context.destination)
          const start = Math.max(context.currentTime, this.playbackTime)
          source.start(start)
          this.playbackTime = start + buffer.duration
        },
        error: (error) => this.handlers.onEvent?.({ type: 'error', text: `TTS 解码失败：${error.message}` }),
      })
      this.decoder.configure({ codec: 'opus', sampleRate: this.sampleRate, numberOfChannels: 1 })
    }
    this.decoder.decode(new EncodedAudioChunk({
      type: 'delta',
      timestamp: Math.round(this.playbackTime * 1_000_000),
      duration: 60_000,
      data: payload,
    }))
  }
}

function downsample(input: Float32Array, fromRate: number, toRate: number): ArrayBuffer {
  if (fromRate === toRate) return toPCM(input)
  const length = Math.max(1, Math.round(input.length * toRate / fromRate))
  const output = new Float32Array(length)
  const ratio = fromRate / toRate
  for (let i = 0; i < length; i += 1) output[i] = input[Math.min(input.length - 1, Math.floor(i * ratio))]
  return toPCM(output)
}

function toPCM(input: Float32Array): ArrayBuffer {
  const output = new Int16Array(input.length)
  for (let i = 0; i < input.length; i += 1) {
    const value = Math.max(-1, Math.min(1, input[i]))
    output[i] = value < 0 ? value * 0x8000 : value * 0x7fff
  }
  return output.buffer
}
