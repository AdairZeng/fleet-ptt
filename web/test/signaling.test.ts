import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  observeSelfTalk,
  parseSignal,
  reduceRosters,
  remoteSpeakers,
  selfTalkAction,
  speakingText,
  type Member,
  type Rosters,
} from '../src/signaling/index.ts'

const captain: Member = { user_id: 'captain', display_name: '队长', talking: false }
const driver: Member = { user_id: 'driver', display_name: '司机乙', talking: false }

test('members and talk events keep the roster for that channel', () => {
  let rosters: Rosters = {}
  const snapshot = parseSignal(
    JSON.stringify({
      type: 'members',
      channel_id: 'ch-1',
      members: [captain, driver],
    }),
  )
  assert.ok(snapshot)
  rosters = reduceRosters(rosters, snapshot)
  assert.deepEqual(remoteSpeakers(rosters['ch-1'], 'driver'), [])

  const start = parseSignal(
    JSON.stringify({ type: 'talk_start', channel_id: 'ch-1', user_id: 'captain' }),
  )
  assert.ok(start)
  rosters = reduceRosters(rosters, start)
  assert.deepEqual(remoteSpeakers(rosters['ch-1'], 'driver'), [
    { ...captain, talking: true },
  ])
  assert.deepEqual(remoteSpeakers(rosters['ch-1'], 'captain'), [])
  assert.equal(selfTalkAction(start, 'ch-1', 'captain'), 'start')
  assert.equal(selfTalkAction(start, 'ch-1', 'driver'), null)
  assert.equal(selfTalkAction(start, 'ch-2', 'captain'), null)

  const other = parseSignal(
    JSON.stringify({
      type: 'members',
      channel_id: 'ch-2',
      members: [{ user_id: 'x', display_name: '别人', talking: true }],
    }),
  )
  assert.ok(other)
  const next = reduceRosters(rosters, other)
  assert.equal(next['ch-1']?.[0]?.talking, true)
  assert.equal(remoteSpeakers(next['ch-2'], 'driver')[0]?.display_name, '别人')

  const end = parseSignal(JSON.stringify({ type: 'talk_end', channel_id: 'ch-1', user_id: 'captain' }))
  assert.ok(end)
  rosters = reduceRosters(next, end)
  assert.equal(rosters['ch-1']?.[0]?.talking, false)
  assert.equal(speakingText(' 队长 '), '队长正在说')
  assert.equal(speakingText(' '), '有人正在说')
})

test('local PTT echo does not publish again, a remote talk_start does', () => {
  const echoed = observeSelfTalk(1, 'start')
  assert.deepEqual(echoed, { suppress: 0, follow: false })
  const remote = observeSelfTalk(0, 'start')
  assert.deepEqual(remote, { suppress: 0, follow: true })
  assert.deepEqual(observeSelfTalk(2, 'end'), { suppress: 2, follow: true })
})

test('parseSignal drops malformed events', () => {
  assert.equal(parseSignal('nope'), null)
  assert.equal(parseSignal(JSON.stringify({ type: 'members', channel_id: 'ch', members: [{}] })), null)
  assert.equal(parseSignal(JSON.stringify({ type: 'talk_start', channel_id: 'ch' })), null)
  assert.deepEqual(parseSignal(JSON.stringify({ type: 'pong' })), { type: 'pong' })
  assert.deepEqual(parseSignal(JSON.stringify({ type: 'channel_created', channel_id: 'ch', name: '装货', join_code: '3842K9' })), {
    type: 'channel_created',
    channel_id: 'ch',
    name: '装货',
    join_code: '3842K9',
  })
  assert.deepEqual(parseSignal(JSON.stringify({ type: 'notice', message: '已切换到装货' })), {
    type: 'notice',
    message: '已切换到装货',
  })
  assert.equal(parseSignal(JSON.stringify({ type: 'notice' })), null)
})

test('invitation events preserve identity and reject malformed payloads', () => {
  const invitation = { id: 'invite-1', channel_id: 'loading', channel_name: '装货', sender_id: 'captain', sender_name: '队长', recipient_id: 'driver', expires_at: '2026-10-24T10:00:00Z', status: 'pending' }
  const event = { type: 'invitation', invitation }
  assert.deepEqual(parseSignal(JSON.stringify(event)), event)
  for (const field of ['id', 'channel_id', 'channel_name', 'sender_id', 'sender_name', 'recipient_id', 'expires_at']) {
    assert.equal(parseSignal(JSON.stringify({ ...event, invitation: { ...invitation, [field]: '' } })), null)
  }
  assert.equal(parseSignal(JSON.stringify({ ...event, invitation: { ...invitation, status: 'unknown' } })), null)
  for (const status of ['accepted', 'rejected', 'expired']) {
    const updated = { ...event, invitation: { ...invitation, status } }
    assert.deepEqual(parseSignal(JSON.stringify(updated)), updated)
  }
})
