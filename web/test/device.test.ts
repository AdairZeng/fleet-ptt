import assert from 'node:assert/strict'
import { test } from 'node:test'
import { initialDeviceSnapshot, reduceDevice } from '../src/device/controller.ts'

test('late media updates cannot power a device back on', () => {
  const off = reduceDevice(initialDeviceSnapshot, { type: 'POWER_TOGGLE' })
  assert.equal(off.phase, 'off')
  assert.equal(reduceDevice(off, { type: 'MEDIA_STATE', ready: true, talking: true, failed: false }), off)
})

test('session restoration alone does not imply a ready media room', () => {
  const off = reduceDevice(initialDeviceSnapshot, { type: 'POWER_TOGGLE' })
  const booting = reduceDevice(off, { type: 'POWER_TOGGLE' })
  const ready = reduceDevice(booting, { type: 'BOOT_READY', hasChannel: false })
  assert.equal(ready.phase, 'standby')
  const listening = reduceDevice(ready, { type: 'MEDIA_STATE', ready: true, talking: false, failed: false })
  assert.equal(listening.phase, 'listening')
  assert.equal(reduceDevice(listening, { type: 'MEDIA_STATE', ready: true, talking: false, failed: false }), listening)
})
