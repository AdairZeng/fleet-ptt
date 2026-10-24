export type DeviceSound = 'boot' | 'ready' | 'confirm' | 'error' | 'transmit'

const tones: Record<DeviceSound, number[]> = {
  boot: [520, 780],
  ready: [660],
  confirm: [660, 660],
  error: [260, 220],
  transmit: [880],
}

// A single local output owned by the powered device. No microphone or cloud TTS.
export class DeviceFeedback {
  private context: AudioContext | null = null
  private cancelPlayback: (() => void) | null = null

  async unlock(): Promise<void> {
    this.context ??= new AudioContext()
    if (this.context.state === 'suspended') await this.context.resume()
  }

  async welcome(signal: AbortSignal): Promise<void> {
    signal.throwIfAborted()
    const context = this.context
    if (!context || context.state !== 'running') throw new Error('请先点按设备以启用声音')
    const response = await fetch('/audio/welcome.wav?v=linx-1', { signal })
    if (!response.ok) throw new Error('开机欢迎语加载失败')
    const buffer = await context.decodeAudioData(await response.arrayBuffer())
    signal.throwIfAborted()
    if (this.context !== context || context.state !== 'running') throw new Error('开机欢迎语播放已停止')
    this.stop()
    await new Promise<void>((resolve, reject) => {
      const source = context.createBufferSource()
      source.buffer = buffer
      source.connect(context.destination)
      let settled = false
      const finish = (error?: Error) => {
        if (settled) return
        settled = true
        window.clearTimeout(timer)
        signal.removeEventListener('abort', cancel)
        source.onended = null
        source.stop()
        source.disconnect()
        if (this.cancelPlayback === cancel) this.cancelPlayback = null
        if (error) reject(error)
        else resolve()
      }
      const cancel = () => finish(new DOMException('已取消欢迎语', 'AbortError'))
      const timer = window.setTimeout(() => finish(new Error('欢迎语播放超时')), (buffer.duration + 2) * 1000)
      this.cancelPlayback = cancel
      signal.addEventListener('abort', cancel, { once: true })
      source.onended = () => finish()
      source.start()
    })
  }

  async play(sound: DeviceSound, signal?: AbortSignal): Promise<void> {
    if (signal?.aborted) throw new DOMException('已取消提示音', 'AbortError')
    const context = this.context
    if (!context || context.state !== 'running') throw new Error('请先点按设备以启用声音')
    this.stop()
    const frequencies = tones[sound]
    const duration = frequencies.length * 0.12
    await new Promise<void>((resolve, reject) => {
      const oscillator = context.createOscillator()
      const gain = context.createGain()
      const start = context.currentTime
      gain.gain.setValueAtTime(0, start)
      gain.gain.linearRampToValueAtTime(0.08, start + 0.01)
      gain.gain.setValueAtTime(0.08, start + duration - 0.02)
      gain.gain.linearRampToValueAtTime(0, start + duration)
      frequencies.forEach((frequency, index) => oscillator.frequency.setValueAtTime(frequency, start + index * 0.12))
      oscillator.connect(gain)
      gain.connect(context.destination)
      let settled = false
      let timer = 0
      const finish = (error?: Error) => {
        if (settled) return
        settled = true
        window.clearTimeout(timer)
        signal?.removeEventListener('abort', cancel)
        oscillator.onended = null
        oscillator.stop()
        oscillator.disconnect()
        gain.disconnect()
        if (this.cancelPlayback === cancel) this.cancelPlayback = null
        if (error) reject(error)
        else resolve()
      }
      const cancel = () => finish(new DOMException('已取消提示音', 'AbortError'))
      this.cancelPlayback = cancel
      signal?.addEventListener('abort', cancel, { once: true })
      oscillator.onended = () => finish()
      oscillator.start(start)
      oscillator.stop(start + duration)
      // A suspended output must never leave a pending publish armed indefinitely.
      timer = window.setTimeout(() => finish(new Error('提示音播放超时，请重试')), 2000)
    })
  }

  stop(): void { this.cancelPlayback?.() }

  close(): void {
    this.stop()
    const context = this.context
    this.context = null
    if (context) void context.close().catch(() => {})
  }
}
