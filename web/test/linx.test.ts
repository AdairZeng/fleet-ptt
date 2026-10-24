import assert from 'node:assert/strict'
import { test } from 'node:test'
import { LinxDevice } from '../src/linx/index.ts'
import { acquireMicrophone, isPlaybackBlocked } from '../src/linx/microphone.ts'
import type { Session } from '../src/state/session.ts'

const tick = () => new Promise<void>((resolve) => setImmediate(resolve))

function environment() {
  const tracks = [{ stopped: false, stop() { this.stopped = true } }]
  const stream = { getTracks: () => tracks } as unknown as MediaStream
  let getMedia: () => Promise<MediaStream> = async () => stream
  const listeners = new Set<() => void>()
  const doc = { hidden: false, addEventListener: (_: string, listener: () => void) => listeners.add(listener), removeEventListener: (_: string, listener: () => void) => listeners.delete(listener) }
  class Socket {
    static OPEN = 1
    static sockets: Socket[] = []
    readyState = 1
    binaryType = ''
    onmessage: ((event: { data: string | ArrayBuffer }) => void) | null = null
    onclose: (() => void) | null = null
    onerror: (() => void) | null = null
    sent: unknown[] = []
    constructor() { Socket.sockets.push(this); queueMicrotask(() => this.event({ type: 'hello', session_id: 's', audio_params: { sample_rate: 16000 } })) }
    event(message: object) { this.onmessage?.({ data: JSON.stringify(message) }) }
    send(value: string | Uint8Array) { this.sent.push(typeof value === 'string' ? JSON.parse(value) : value) }
    close() { this.readyState = 3; this.onclose?.() }
  }
  class Node {
    connect() {}
    disconnect() {}
  }
  class Source extends Node {
    onended: (() => void) | null = null
    buffer: unknown
    stopped = false
    start() {}
    stop() { this.stopped = true }
    end() { this.onended?.() }
  }
  class Context {
    static contexts: Context[] = []
    sampleRate = 16000
    currentTime = 0
    destination = {}
    closed = false
    processor = Object.assign(new Node(), { onaudioprocess: null as ((event: object) => void) | null })
    sources: Source[] = []
    constructor() { Context.contexts.push(this) }
    async resume() {}
    async close() { this.closed = true }
    createMediaStreamSource() { return new Node() }
    createScriptProcessor() { return this.processor }
    createBuffer() { return { duration: 0.06, copyToChannel() {} } }
    createBufferSource() { const source = new Source(); this.sources.push(source); return source }
  }
  let finishFlush = () => {}
  class Decoder {
    state = 'configured'
    readonly output: (audio: object) => void
    constructor(init: { output: (audio: object) => void }) { this.output = init.output }
    configure() {}
    close() { this.state = 'closed' }
    decode(chunk: { type: string }) {
      // WebCodecs requires every Opus packet to be a key chunk.
      if (chunk.type !== 'key') throw new DOMException('A key frame is required after configure()', 'DataError')
      this.output({ numberOfChannels: 1, numberOfFrames: 960, sampleRate: 16000, copyTo() {}, close() {} })
    }
    flush() { return new Promise<void>((resolve) => { finishFlush = resolve }) }
  }
  class Chunk {
    readonly type: string
    constructor(init: { type: string }) { this.type = init.type }
  }
  const globals = { WebSocket: Socket, AudioContext: Context, AudioDecoder: Decoder, EncodedAudioChunk: Chunk, location: { protocol: 'http:', host: 'localhost' }, navigator: { mediaDevices: { getUserMedia: () => getMedia() } }, document: doc }
  const original = new Map<string, PropertyDescriptor | undefined>()
  for (const [name, value] of Object.entries(globals)) {
    original.set(name, Object.getOwnPropertyDescriptor(globalThis, name))
    Object.defineProperty(globalThis, name, { value, configurable: true, writable: true })
  }
  return { stream, tracks, Context, Socket, getMedia: (next: () => Promise<MediaStream>) => { getMedia = next }, flush: () => finishFlush(), hide: (hidden: boolean) => { doc.hidden = hidden; listeners.forEach((listener) => listener()) }, restore: () => { original.forEach((descriptor, name) => { if (descriptor) Object.defineProperty(globalThis, name, descriptor); else Reflect.deleteProperty(globalThis, name) }) } }
}
const session = { session_token: 'test' } as Session

test('power-off while permission is pending releases capture when it arrives', async () => {
  const env = environment()
  let allow: (stream: MediaStream) => void = () => {}
  env.getMedia(() => new Promise((resolve) => { allow = resolve }))
  const device = new LinxDevice(session)
  try {
    const starting = device.start()
    await tick()
    device.close()
    allow(env.stream)
    await starting
    assert.equal(env.tracks[0].stopped, true)
    assert.equal(env.Context.contexts[0].closed, true)
    assert.equal(env.Context.contexts[0].processor.onaudioprocess, null)
  } finally { device.close(); env.restore() }
})

test('wake acknowledges only after RTN stop and cancellation invalidates pending acknowledgement', async () => {
  const env = environment()
  let stopped: () => void = () => {}
  const device = new LinxDevice(session, { onWake: () => new Promise((resolve) => { stopped = resolve }) })
  try {
    await device.start()
    const socket = env.Socket.sockets[0]
    socket.event({ type: 'wake', state: 'detected', turn_id: 'one' })
    await tick()
    assert.equal(socket.sent.some((value: any) => value.state === 'ready'), false)
    stopped()
    await tick()
    assert.ok(socket.sent.some((value: any) => value.state === 'ready' && value.turn_id === 'one'))
    socket.event({ type: 'wake', state: 'detected', turn_id: 'two' })
    await tick()
    device.stop()
    stopped()
    await tick()
    assert.equal(socket.sent.some((value: any) => value.state === 'ready' && value.turn_id === 'two'), false)
    assert.equal(env.Context.contexts.length, 1)
  } finally { device.close(); env.restore() }
})

test('TTS keeps capture and RTN blocked until decoder flush and actual playback complete', async () => {
  const env = environment()
  const device = new LinxDevice(session)
  try {
    await device.start()
    const socket = env.Socket.sockets[0]
    const context = env.Context.contexts[0]
    socket.event({ type: 'wake', state: 'detected', turn_id: 'one' })
    await tick()
    socket.event({ type: 'tts', state: 'start' })
    socket.onmessage?.({ data: new ArrayBuffer(4) })
    socket.event({ type: 'tts', state: 'stop' })
    assert.equal(isPlaybackBlocked(), true)
    const before = socket.sent.length
    context.processor.onaudioprocess?.({ inputBuffer: { getChannelData: () => new Float32Array(640) } })
    assert.equal(socket.sent.length, before)
    env.flush()
    await tick()
    assert.equal(isPlaybackBlocked(), true)
    assert.equal(socket.sent.some((value: any) => value.state === 'playback_done'), false)
    context.sources[0].end()
    assert.equal(isPlaybackBlocked(), false)
    assert.ok(socket.sent.some((value: any) => value.state === 'playback_done' && value.turn_id === 'one'))
    socket.event({ type: 'wake', state: 'followup' })
    const resumed = socket.sent.length
    context.processor.onaudioprocess?.({ inputBuffer: { getChannelData: () => new Float32Array(640) } })
    assert.ok(socket.sent.length > resumed)
  } finally { device.close(); env.restore() }
})

test('shared capture is stopped only when its last consumer releases it', async () => {
  const env = environment()
  try {
    const voice = await acquireMicrophone()
    const rtc = await acquireMicrophone()
    assert.equal(voice.stream, rtc.stream)
    voice.release()
    assert.equal(env.tracks[0].stopped, false)
    rtc.release()
    assert.equal(env.tracks[0].stopped, true)
    rtc.release()
  } finally { env.restore() }
})

test('hidden page suspends capture and foreground resumes with the same audio context', async () => {
  const env = environment()
  const events: string[] = []
  const device = new LinxDevice(session, { onEvent: (event) => events.push(event.type) })
  try {
    await device.start()
    env.hide(true)
    assert.equal(env.Context.contexts[0].processor.onaudioprocess, null)
    assert.ok(events.includes('suspended'))
    env.hide(false)
    await tick()
    assert.equal(env.Context.contexts.length, 1)
    assert.equal(events.filter((event) => event === 'ready').length, 2)
  } finally { device.close(); env.restore() }
})

test('unavailable Opus decoder cancels the turn and releases playback blocking immediately', async () => {
  const env = environment()
  const errors: string[] = []
  const device = new LinxDevice(session, { onEvent: (event) => { if (event.type === 'error') errors.push(event.text ?? '') } })
  try {
    await device.start()
    Reflect.deleteProperty(globalThis, 'AudioDecoder')
    const socket = env.Socket.sockets[0]
    socket.event({ type: 'wake', state: 'detected', turn_id: 'one' })
    await tick()
    socket.event({ type: 'tts', state: 'start' })
    socket.onmessage?.({ data: new ArrayBuffer(4) })
    assert.equal(isPlaybackBlocked(), false)
    assert.ok(socket.sent.some((value: any) => value.type === 'wake' && value.state === 'cancel'))
    assert.ok(socket.sent.some((value: any) => value.type === 'abort'))
    assert.ok(errors.some((message) => message.includes('不支持 Opus')))
  } finally { device.close(); env.restore() }
})

test('remote disconnect releases microphone and allows an explicit reconnect', async () => {
  const env = environment()
  const events: string[] = []
  const device = new LinxDevice(session, { onEvent: (event) => events.push(event.type) })
  try {
    await device.start()
    env.Socket.sockets[0].close()
    assert.equal(env.tracks[0].stopped, true)
    assert.ok(events.includes('disconnected'))
    await device.start()
    assert.equal(env.Socket.sockets.length, 2)
    assert.equal(env.Context.contexts.length, 1)
    assert.equal(events.filter((event) => event === 'ready').length, 2)
  } finally { device.close(); env.restore() }
})

test('cancel ignores already queued speech frames from the canceled turn', async () => {
  const env = environment()
  const device = new LinxDevice(session)
  try {
    await device.start()
    const socket = env.Socket.sockets[0]
    socket.event({ type: 'wake', state: 'detected', turn_id: 'one' })
    await tick()
    device.stop()
    socket.event({ type: 'tts', state: 'start' })
    socket.onmessage?.({ data: new ArrayBuffer(4) })
    socket.event({ type: 'tts', state: 'stop' })
    await tick()
    assert.equal(isPlaybackBlocked(), false)
    assert.equal(env.Context.contexts[0].sources.length, 0)
    assert.equal(socket.sent.some((value: any) => value.state === 'playback_done'), false)
  } finally { device.close(); env.restore() }
})

test('capture stays 16 kHz when Linx negotiates 24 kHz output', async () => {
  const env = environment()
  const device = new LinxDevice(session)
  try {
    await device.start()
    const socket = env.Socket.sockets[0]
    socket.event({ type: 'hello', audio_params: { sample_rate: 24000 } })
    const context = env.Context.contexts[0]
    context.sampleRate = 48000
    context.processor.onaudioprocess?.({ inputBuffer: { getChannelData: () => new Float32Array(1920) } })
    const frames = socket.sent.filter((value) => value instanceof Uint8Array)
    // 40 ms at 48 kHz becomes 640 mono samples / 1280 bytes at 16 kHz.
    assert.equal(frames.length, 2)
    assert.ok(frames.every((frame: Uint8Array) => frame.byteLength === 640))
  } finally { device.close(); env.restore() }
})

test('retry signals ready when continuous microphone capture is already active', async () => {
  const env = environment()
  let ready = 0
  const device = new LinxDevice(session, { onEvent: (event) => { if (event.type === 'ready') ready += 1 } })
  try {
    await device.start()
    await device.start()
    assert.equal(ready, 2)
    assert.equal(env.Socket.sockets.length, 1)
    assert.equal(env.Context.contexts.length, 1)
  } finally { device.close(); env.restore() }
})

test('late wake while hidden is canceled without acknowledgement or playback', async () => {
  const env = environment()
  let wakes = 0
  const device = new LinxDevice(session, { onWake: async () => { wakes += 1 } })
  try {
    await device.start()
    env.hide(true)
    const socket = env.Socket.sockets[0]
    const before = socket.sent.length
    socket.event({ type: 'wake', state: 'detected', turn_id: 'late' })
    socket.event({ type: 'tts', state: 'start' })
    socket.onmessage?.({ data: new ArrayBuffer(4) })
    await tick()
    const messages = socket.sent.slice(before) as { type?: string; state?: string }[]
    assert.equal(wakes, 0)
    assert.ok(messages.some((message) => message.type === 'wake' && message.state === 'cancel'))
    assert.equal(messages.some((message) => message.state === 'ready' || message.state === 'start'), false)
    assert.equal(env.Context.contexts[0].sources.length, 0)
    assert.equal(env.Context.contexts[0].processor.onaudioprocess, null)
    assert.equal(isPlaybackBlocked(), false)
  } finally { device.close(); env.restore() }
})
