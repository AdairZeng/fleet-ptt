import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { test } from 'node:test'
import { toBuffer } from 'qrcode'
import jsQR from 'jsqr'
import {
  invalidJoinQRMessage,
  joinQROptions,
  joinURL,
  readJoinLocation,
  readJoinPayload,
} from '../src/state/joinLink.ts'

const require = createRequire(import.meta.url)
const { PNG } = require('pngjs') as {
  PNG: { sync: { read: (buf: Buffer) => { width: number; height: number; data: Buffer } } }
}

test('join location accepts a code path or join query and rejects a bad payload', () => {
  assert.deepEqual(readJoinLocation({ pathname: '/join/3842k9', search: '' }), {
    ok: true,
    code: '3842K9',
  })
  assert.deepEqual(readJoinLocation({ pathname: '/', search: '?join=ABCDEF' }), {
    ok: true,
    code: 'ABCDEF',
  })
  assert.equal(readJoinLocation({ pathname: '/', search: '' }), null)
  assert.deepEqual(readJoinLocation({ pathname: '/join/000000', search: '' }), {
    ok: false,
    message: invalidJoinQRMessage,
  })
  assert.deepEqual(readJoinLocation({ pathname: '/join', search: '' }), {
    ok: false,
    message: invalidJoinQRMessage,
  })
  assert.deepEqual(readJoinLocation({ pathname: '/', search: '?join=' }), {
    ok: false,
    message: invalidJoinQRMessage,
  })
})

test('join payload accepts a raw code or absolute join url', () => {
  const url = joinURL('https://fleet.example', '3842K9')
  assert.equal(url, 'https://fleet.example/join/3842K9')
  assert.deepEqual(readJoinPayload(url), { ok: true, code: '3842K9' })
  assert.deepEqual(readJoinPayload(' 3842k9 '), { ok: true, code: '3842K9' })
  assert.equal(readJoinPayload(''), null)
  assert.equal(readJoinPayload('   '), null)
  assert.deepEqual(readJoinPayload('https://example.com/not-a-join'), {
    ok: false,
    message: invalidJoinQRMessage,
  })
  assert.deepEqual(readJoinPayload('这不是二维码'), {
    ok: false,
    message: invalidJoinQRMessage,
  })
})

test('join qr can be decoded back to the join url', async () => {
  const url = joinURL('http://192.168.1.20:8080', '3842K9')
  const png = PNG.sync.read(await toBuffer(url, joinQROptions))
  const decoded = jsQR(new Uint8ClampedArray(png.data), png.width, png.height)
  assert.equal(decoded?.data, url)
  assert.deepEqual(readJoinPayload(decoded?.data ?? ''), { ok: true, code: '3842K9' })
})
