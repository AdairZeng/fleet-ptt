import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  idlePresence,
  memberCountText,
  memberIsLive,
  presenceCopy,
  talkPresence,
} from '../src/state/presence.ts'

test('listening, commanding, and talking use distinct labels', () => {
  assert.equal(
    presenceCopy(talkPresence('ready', 'in_channel', false)).label,
    '可听',
  )
  assert.equal(
    presenceCopy(talkPresence('ready', 'talking', false)).label,
    '正在讲',
  )
  assert.equal(
    presenceCopy(talkPresence('ready', 'in_channel', true)).label,
    '指令中',
  )
  assert.equal(
    presenceCopy(talkPresence('ready', 'talking', true)).label,
    '指令中',
  )
  const labels = ['可听', '指令中', '正在讲']
  assert.equal(new Set(labels).size, labels.length)
})

test('related room states keep their existing copy', () => {
  assert.equal(talkPresence('joining', 'out', false), 'joining')
  assert.equal(presenceCopy('joining').detail, '正在进入频道，只收听…')
  assert.equal(talkPresence('error', 'talking', true), 'offline')
  assert.equal(presenceCopy('offline').label, '还没进房')
  assert.equal(talkPresence('ready', 'out', false), 'listening')
  assert.equal(presenceCopy(idlePresence(false)).label, '未进频道')
  assert.equal(presenceCopy(idlePresence(true)).label, '指令中')
  assert.match(presenceCopy('commanding').detail, /灵矽设备上说指令/)
})

test('member count and live flag follow the roster', () => {
  assert.equal(memberCountText(undefined), '…')
  assert.equal(memberCountText(0), '0 人')
  assert.equal(memberCountText(2), '2 人')
  assert.equal(memberIsLive('me', false, 'me', true), true)
  assert.equal(memberIsLive('other', true, 'me', false), true)
  assert.equal(memberIsLive('other', false, 'me', true), false)
})
