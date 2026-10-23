import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  readChannelList,
  switchListeningChannel,
  type Channel,
} from '../src/state/channel.ts'

const loading: Channel = {
  channel_id: 'ch-1',
  name: '装货',
  join_code: '3842K9',
  role: 'member',
}

test('readChannelList keeps joined channels and rejects a broken payload', () => {
  assert.deepEqual(
    readChannelList({
      channels: [loading, { channel_id: 'ch-2', name: '卸货', join_code: 'ABCDEF', role: 'owner' }],
    }),
    [loading, { channel_id: 'ch-2', name: '卸货', join_code: 'ABCDEF', role: 'owner' }],
  )
  assert.deepEqual(readChannelList({ channels: [] }), [])
  assert.equal(readChannelList({ error: '未登录' }), null)
  assert.equal(readChannelList({ channels: [{ name: '装货' }] }), null)
})

test('switch stops uplink, changes channel, then leaves the old room', async () => {
  const steps: string[] = []
  const next = await switchListeningChannel(
    async () => {
      steps.push('stop')
    },
    async () => {
      steps.push('switch')
      return loading
    },
    async () => {
      steps.push('leave')
    },
  )
  assert.equal(next, loading)
  assert.deepEqual(steps, ['stop', 'switch', 'leave'])

  steps.length = 0
  await assert.rejects(
    switchListeningChannel(
      async () => {
        steps.push('stop')
        throw new Error('麦克风还开着')
      },
      async () => {
        steps.push('switch')
        return loading
      },
      async () => {
        steps.push('leave')
      },
    ),
    /麦克风还开着/,
  )
  assert.deepEqual(steps, ['stop'])
})
