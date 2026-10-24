import type { Session } from '../state/session.ts'
import { acquireMicrophone, setPlaybackBlocked } from './microphone.ts'

const INPUT_SAMPLE_RATE = 16000

type LinxEvent = {
  type: string
  state?: string
  text?: string
  session_id?: string
  turn_id?: string
  is_aborted?: boolean
}

export type LinxHandlers = {
  onEvent?: (event: LinxEvent) => void
  onBinary?: (payload: ArrayBuffer) => void
  onWake?: () => Promise<void>
}

// Continuous cloud recognition uses the same transport as the hardware device.
// The server gates agent output and business tools until a wake turn is accepted.
export class LinxDevice {
  private readonly session: Session
  private readonly handlers: LinxHandlers
  private socket: WebSocket | null = null
  private connection: Promise<void> | null = null
  private cancelConnection: (() => void) | null = null
  private audioContext: AudioContext | null = null
  private source: MediaStreamAudioSourceNode | null = null
  private processor: ScriptProcessorNode | null = null
  private releaseMicrophone: (() => void) | null = null
  private sessionID = ''
  private outputFormat = 'opus'
  private outputSampleRate = 16000
  private pendingPCM = new Uint8Array(0)
  private decoder: AudioDecoder | null = null
  private playbackTime = 0
  private playbackSources = new Set<AudioBufferSourceNode>()
  private playbackEpoch = 0
  private playbackEnded = false
  private playbackActive = false
  private capturing = false
  private closed = false
  private generation = 0
  private turnID = ''
  private starting: Promise<void> | null = null
  private observingVisibility = false
  private readonly visibilityChanged = () => {
    if (document.hidden) {
      this.generation += 1
      this.turnID = ''
      this.releaseCapture()
      this.clearPlayback()
      if (this.socket?.readyState === WebSocket.OPEN) {
        this.send({ type: 'wake', state: 'cancel', session_id: this.sessionID })
        this.send({ type: 'abort', session_id: this.sessionID })
        this.send({ type: 'listen', state: 'stop', session_id: this.sessionID })
      }
      this.handlers.onEvent?.({ type: 'suspended' })
    } else {
      // Await any in-flight permission prompt before reacquiring its lease.
      void (this.starting ?? Promise.resolve()).catch(() => {}).then(() => this.start()).catch((error: unknown) => {
        if (!this.closed) this.handlers.onEvent?.({ type: 'error', text: error instanceof Error ? error.message : '麦克风恢复失败' })
      })
    }
  }

  constructor(session: Session, handlers: LinxHandlers = {}) {
    this.session = session
    this.handlers = handlers
  }

  connect(): Promise<void> {
    if (this.closed) return Promise.reject(new Error('设备已关闭'))
    if (this.connection) return this.connection
    const protocol = location.protocol === 'https:' ? 'wss' : 'ws'
    const socket = new WebSocket(`${protocol}://${location.host}/api/v1/linx/ws?token=${encodeURIComponent(this.session.session_token)}`)
    socket.binaryType = 'arraybuffer'
    this.socket = socket
    this.connection = new Promise<void>((resolve, reject) => {
      const timer = setTimeout(() => { reject(new Error('灵汐握手超时')); socket.close() }, 10000)
      this.cancelConnection = () => { clearTimeout(timer); reject(new Error('灵汐连接已关闭')) }
      socket.onerror = () => { clearTimeout(timer); reject(new Error('灵汐连接失败')); socket.close() }
      socket.onclose = () => {
        clearTimeout(timer)
        reject(new Error('灵汐连接已关闭'))
        if (this.socket !== socket) return
        this.socket = null
        this.connection = null
        this.cancelConnection = null
        this.generation += 1
        this.releaseCapture()
        this.clearPlayback()
        if (!this.closed) this.handlers.onEvent?.({ type: 'disconnected' })
      }
      socket.onmessage = (event) => {
        if (this.socket !== socket || this.closed) return
        if (typeof event.data === 'string') {
          let message: LinxEvent & { audio_params?: { sample_rate?: number; format?: string } }
          try { message = JSON.parse(event.data) } catch {
            this.handlers.onEvent?.({ type: 'error', text: '灵汐消息格式无效' })
            return
          }
          if (message.session_id) this.sessionID = message.session_id
          if (message.type === 'hello') {
            this.outputFormat = message.audio_params?.format ?? 'opus'
            if (message.audio_params?.sample_rate) this.outputSampleRate = message.audio_params.sample_rate
            clearTimeout(timer)
            resolve()
          }
          this.handleEvent(message)
        } else if (event.data instanceof ArrayBuffer && this.turnID && !document.hidden) {
          this.playTTS(event.data)
          this.handlers.onBinary?.(event.data)
        }
      }
    })
    return this.connection
  }

  // Call synchronously from a gesture, even if an earlier resume is still pending.
  unlockAudio(): void {
    if (this.closed) return
    this.audioContext ??= new AudioContext()
    void this.audioContext.resume().catch((error: unknown) => {
      if (!this.closed) this.handlers.onEvent?.({ type: 'error', text: error instanceof Error ? error.message : '请点击指令键开启声音' })
    })
  }

  start(): Promise<void> {
    this.unlockAudio()
    if (this.starting) return this.starting
    if (this.processor && !document.hidden && this.socket?.readyState === WebSocket.OPEN) {
      this.handlers.onEvent?.({ type: 'ready' })
      return Promise.resolve()
    }
    this.starting = this.startCapture().finally(() => { this.starting = null })
    return this.starting
  }

  private async startCapture(): Promise<void> {
    const generation = this.generation
    if (this.closed) throw new Error('设备已关闭')
    if (!this.observingVisibility) {
      document.addEventListener('visibilitychange', this.visibilityChanged)
      this.observingVisibility = true
    }
    if (document.hidden) { this.handlers.onEvent?.({ type: 'suspended' }); return }
    this.audioContext ??= new AudioContext()
    await this.audioContext.resume()
    if (this.closed || generation !== this.generation) return
    await this.connect()
    if (this.closed || generation !== this.generation) return
    const lease = await acquireMicrophone()
    if (this.closed || generation !== this.generation) { lease.release(); return }
    this.releaseMicrophone = lease.release
    this.source = this.audioContext.createMediaStreamSource(lease.stream)
    this.processor = this.audioContext.createScriptProcessor(2048, 1, 1)
    this.processor.onaudioprocess = (event) => {
      if (!this.capturing || this.socket?.readyState !== WebSocket.OPEN) return
      const input = event.inputBuffer.getChannelData(0)
      const pcm = new Uint8Array(downsample(input, this.audioContext?.sampleRate ?? 48000, INPUT_SAMPLE_RATE))
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
    this.source.connect(this.processor)
    this.processor.connect(this.audioContext.destination)
    this.resumeCapture()
    this.handlers.onEvent?.({ type: 'ready' })
  }

  async listen(): Promise<void> {
    await this.start()
    if (this.closed || document.hidden) return
    this.turnID = ''
    this.clearPlayback()
    this.send({ type: 'wake', state: 'manual', session_id: this.sessionID })
    this.resumeCapture()
  }

  // Closing a command returns to wake standby. Power-off uses close().
  stop(): void {
    this.turnID = ''
    this.clearPlayback()
    if (this.socket?.readyState !== WebSocket.OPEN) return
    this.send({ type: 'wake', state: 'cancel', session_id: this.sessionID })
    this.send({ type: 'abort', session_id: this.sessionID })
    this.resumeCapture()
  }

  close(): void {
    this.closed = true
    document.removeEventListener('visibilitychange', this.visibilityChanged)
    this.observingVisibility = false
    this.generation += 1
    this.cancelConnection?.()
    this.cancelConnection = null
    this.releaseCapture()
    this.clearPlayback()
    this.socket?.close()
    this.socket = null
    this.connection = null
    void this.audioContext?.close()
    this.audioContext = null
  }

  private handleEvent(message: LinxEvent): void {
    if (message.type === 'wake') {
      if (message.state === 'detected') {
        if (document.hidden) { this.stop(); return }
        this.capturing = false
        this.pendingPCM = new Uint8Array(0)
        this.turnID = message.turn_id ?? ''
        const generation = this.generation
        const turnID = this.turnID
        void Promise.resolve().then(() => this.handlers.onWake?.()).then(() => {
          if (this.closed || document.hidden || generation !== this.generation || this.turnID !== turnID) return
          this.send({ type: 'wake', state: 'ready', session_id: this.sessionID, turn_id: turnID })
        }).catch((error: unknown) => {
          if (this.closed || document.hidden || generation !== this.generation || this.turnID !== turnID) return
          this.stop()
          this.handlers.onEvent?.({ type: 'error', text: error instanceof Error ? error.message : '停止对讲失败' })
        })
      } else if (message.state === 'standby' || message.state === 'followup') {
        this.turnID = ''
        if (message.state === 'standby') this.clearPlayback()
        if (!this.playbackActive) this.resumeCapture()
      }
    }
    if (message.type === 'tts') {
      if (!this.turnID || document.hidden) return
      if (message.state === 'start') this.beginPlayback()
      if (message.state === 'stop') {
        if (message.is_aborted) { this.clearPlayback(); this.finishTurn() }
        else void this.drainPlayback()
      }
    }
    this.handlers.onEvent?.(message)
  }

  private send(message: object): void {
    if (this.socket?.readyState !== WebSocket.OPEN) throw new Error('灵汐连接未就绪')
    this.socket.send(JSON.stringify(message))
  }

  private resumeCapture(): void {
    if (this.closed || document.hidden || !this.processor || this.playbackActive || this.socket?.readyState !== WebSocket.OPEN) return
    this.pendingPCM = new Uint8Array(0)
    this.capturing = true
    this.send({ session_id: this.sessionID, type: 'listen', state: 'start', mode: 'realtime' })
  }

  private releaseCapture(): void {
    this.capturing = false
    this.pendingPCM = new Uint8Array(0)
    if (this.processor) this.processor.onaudioprocess = null
    this.processor?.disconnect()
    this.source?.disconnect()
    this.releaseMicrophone?.()
    this.releaseMicrophone = null
    this.processor = null
    this.source = null
  }

  private beginPlayback(): void {
    this.capturing = false
    this.pendingPCM = new Uint8Array(0)
    this.playbackEnded = false
    if (this.playbackActive) return
    this.playbackActive = true
    setPlaybackBlocked(true)
    this.handlers.onEvent?.({ type: 'playback', state: 'start' })
  }

  private clearPlayback(): void {
    this.playbackEpoch += 1
    if (this.decoder && this.decoder.state !== 'closed') this.decoder.close()
    this.decoder = null
    for (const source of this.playbackSources) { source.onended = null; source.stop(); source.disconnect() }
    this.playbackSources.clear()
    this.playbackTime = 0
    this.playbackEnded = false
    const active = this.playbackActive
    this.playbackActive = false
    setPlaybackBlocked(false)
    if (active) this.handlers.onEvent?.({ type: 'playback', state: 'stop' })
  }

  private async drainPlayback(): Promise<void> {
    const epoch = this.playbackEpoch
    try { await this.decoder?.flush() } catch {
      if (epoch !== this.playbackEpoch) return
      this.stop()
      this.handlers.onEvent?.({ type: 'error', text: 'TTS 解码失败' })
      return
    }
    if (epoch !== this.playbackEpoch || this.closed) return
    this.playbackEnded = true
    if (this.playbackSources.size === 0) { this.clearPlayback(); this.finishTurn() }
  }

  private finishTurn(): void {
    if (this.socket?.readyState !== WebSocket.OPEN || this.closed) return
    this.send({ type: 'wake', state: 'playback_done', session_id: this.sessionID, turn_id: this.turnID })
  }

  private schedulePlayback(buffer: AudioBuffer): void {
    const context = this.audioContext!
    const source = context.createBufferSource()
    source.buffer = buffer
    source.connect(context.destination)
    this.playbackSources.add(source)
    source.onended = () => {
      source.disconnect()
      this.playbackSources.delete(source)
      if (this.playbackEnded && this.playbackSources.size === 0) { this.clearPlayback(); this.finishTurn() }
    }
    const start = Math.max(context.currentTime, this.playbackTime)
    source.start(start)
    this.playbackTime = start + buffer.duration
  }

  private playTTS(payload: ArrayBuffer): void {
    this.beginPlayback()
    this.audioContext ??= new AudioContext()
    if (this.outputFormat === 'pcm') {
      if (payload.byteLength === 0 || payload.byteLength % 2 !== 0) {
        this.stop()
        this.handlers.onEvent?.({ type: 'error', text: '灵汐 PCM 音频帧长度无效' })
        return
      }
      const samples = new Float32Array(payload.byteLength / 2)
      const pcm = new DataView(payload)
      for (let i = 0; i < samples.length; i += 1) samples[i] = pcm.getInt16(i * 2, true) / 32768
      const buffer = this.audioContext.createBuffer(1, samples.length, this.outputSampleRate)
      buffer.copyToChannel(samples, 0)
      this.schedulePlayback(buffer)
      return
    }
    if (this.outputFormat !== 'opus') {
      this.stop()
      this.handlers.onEvent?.({ type: 'error', text: `不支持灵汐返回的音频格式：${this.outputFormat}` })
      return
    }
    if (typeof AudioDecoder === 'undefined' || typeof EncodedAudioChunk === 'undefined') {
      this.stop()
      this.handlers.onEvent?.({ type: 'error', text: '浏览器不支持 Opus 语音播放，请使用新版 Chrome 或 Edge' })
      return
    }
    this.audioContext ??= new AudioContext()
    if (!this.decoder) {
      const epoch = this.playbackEpoch
      this.decoder = new AudioDecoder({
        output: (audio) => {
          const context = this.audioContext
          if (!context || epoch !== this.playbackEpoch) { audio.close(); return }
          const buffer = context.createBuffer(audio.numberOfChannels, audio.numberOfFrames, audio.sampleRate)
          for (let channel = 0; channel < audio.numberOfChannels; channel += 1) {
            const samples = new Float32Array(audio.numberOfFrames)
            audio.copyTo(samples, { planeIndex: channel })
            buffer.copyToChannel(samples, channel)
          }
          audio.close()
          this.schedulePlayback(buffer)
        },
        error: (error) => {
          if (epoch !== this.playbackEpoch) return
          this.stop()
          this.handlers.onEvent?.({ type: 'error', text: `TTS 解码失败：${error.message}` })
        },
      })
      try {
        this.decoder.configure({ codec: 'opus', sampleRate: this.outputSampleRate, numberOfChannels: 1 })
      } catch {
        this.stop()
        this.handlers.onEvent?.({ type: 'error', text: 'TTS 解码器初始化失败' })
        return
      }
    }
    try {
      // Every Opus packet is a key chunk, including the first after configure().
      this.decoder.decode(new EncodedAudioChunk({ type: 'key', timestamp: Math.round(this.playbackTime * 1_000_000), duration: 60_000, data: payload }))
    } catch {
      this.stop()
      this.handlers.onEvent?.({ type: 'error', text: 'TTS 解码失败' })
    }
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
