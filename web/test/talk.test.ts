import assert from 'node:assert/strict'
import { test } from 'node:test'
import { reduceTalk } from '../src/state/talk.ts'

test('join listens, PTT talks, release or end returns to listening', () => {
  assert.equal(reduceTalk('out', 'JOINED_CHANNEL'), 'in_channel')
  assert.equal(reduceTalk('in_channel', 'TALK_START'), 'talking')
  assert.equal(reduceTalk('talking', 'TALK_END'), 'in_channel')
  assert.equal(reduceTalk('in_channel', 'TALK_END'), 'in_channel')
})

test('cannot talk before join, and leave always drops the room', () => {
  assert.equal(reduceTalk('out', 'TALK_START'), 'out')
  assert.equal(reduceTalk('out', 'TALK_END'), 'out')
  assert.equal(reduceTalk('talking', 'LEFT'), 'out')
  assert.equal(reduceTalk('in_channel', 'LEFT'), 'out')
  assert.equal(reduceTalk('talking', 'JOINED_CHANNEL'), 'talking')
})
