import assert from 'node:assert/strict'
import { test } from 'node:test'
import { DeviceFeedback } from '../src/device/feedback.ts'

function environment() {
  const sources: { onended: (() => void) | null; stopped: boolean; start(): void; stop(): void; connect(): void; disconnect(): void; buffer?: unknown }[] = []
  class Context {
    state = 'running'
    destination = {}
    async resume() {}
    async close() { this.state = 'closed' }
    async decodeAudioData() { return { duration: 2.8 } }
    createBufferSource() {
      const source = { onended: null as (() => void) | null, stopped: false, start() {}, stop() { this.stopped = true }, connect() {}, disconnect() {} }
      sources.push(source)
      return source
    }
  }
  const originals = new Map<string, PropertyDescriptor | undefined>()
  for (const [key, value] of Object.entries({ AudioContext: Context, window: { setTimeout, clearTimeout }, fetch: async () => ({ ok: true, arrayBuffer: async () => new ArrayBuffer(0) }) })) {
    originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key))
    Object.defineProperty(globalThis, key, { configurable: true, writable: true, value })
  }
  return { sources, restore() { originals.forEach((value, key) => { if (value) Object.defineProperty(globalThis, key, value); else Reflect.deleteProperty(globalThis, key) }) } }
}
const tick = () => new Promise<void>((resolve) => setImmediate(resolve))

test('welcome waits until audio actually finishes before completing startup', async () => {
  const env = environment(); const feedback = new DeviceFeedback()
  try {
    await feedback.unlock()
    let complete = false
    const welcome = feedback.welcome(new AbortController().signal).then(() => { complete = true })
    await tick()
    assert.equal(complete, false)
    env.sources[0].onended?.()
    await welcome
    assert.equal(complete, true)
  } finally { feedback.close(); env.restore() }
})

test('power-off abort stops welcome immediately', async () => {
  const env = environment(); const feedback = new DeviceFeedback()
  try {
    await feedback.unlock()
    const abort = new AbortController()
    const welcome = feedback.welcome(abort.signal)
    const rejected = assert.rejects(welcome, { name: 'AbortError' })
    await tick()
    abort.abort()
    await rejected
    assert.equal(env.sources[0].stopped, true)
  } finally { feedback.close(); env.restore() }
})

test('failed welcome asset rejects without starting audio', async () => {
  const env = environment(); const feedback = new DeviceFeedback()
  try {
    await feedback.unlock()
    globalThis.fetch = async () => ({ ok: false }) as Response
    await assert.rejects(feedback.welcome(new AbortController().signal), /欢迎语加载失败/)
    assert.equal(env.sources.length, 0)
  } finally { feedback.close(); env.restore() }
})
