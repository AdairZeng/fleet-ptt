import assert from 'node:assert/strict'
import { test } from 'node:test'
import { commandToggle } from '../src/state/command.ts'

test('opening command stops uplink, closing command does not start it', () => {
  assert.deepEqual(commandToggle(false), { commanding: true, stopUplink: true })
  assert.deepEqual(commandToggle(true), { commanding: false, stopUplink: false })
})
