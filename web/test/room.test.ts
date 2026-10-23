import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  RtnRoom,
  type LocalMic,
  type RemoteAudio,
  type RemoteTrack,
  type RtnConnection,
  type RtnEvents,
  type RtnTransport,
} from '../src/rtc/room.ts'

test('join only subscribes and does not publish', async () => {
  const fake = new FakeTransport()
  const speakers: string[][] = []
  const room = new RtnRoom(fake, (ids) => {
    speakers.push(ids)
  })
  await room.join('room-token')
  assert.deepEqual(fake.calls, ['join'])
  assert.equal(fake.mics, 0)
  assert.deepEqual(fake.subscribed, [['a1']])
  assert.deepEqual(fake.played, ['a1'])
  assert.deepEqual(speakers, [['driver-2']])

  fake.events.onUnpublished(fake.conn, 'driver-2', [track('a1', 'driver-2', true)])
  assert.deepEqual(fake.stopped, ['a1'])
  assert.deepEqual(speakers.at(-1), [])
})

test('PTT publishes once and release or end unpublishes', async () => {
  const fake = new FakeTransport()
  const room = new RtnRoom(fake)
  await room.join('room-token')
  assert.equal(await room.startPublish(), 'published')
  assert.equal(await room.startPublish(), 'published')
  assert.equal(fake.mics, 1)
  assert.deepEqual(
    fake.calls.filter((call) => call === 'publish'),
    ['publish'],
  )
  await room.stopPublish()
  await room.stopPublish()
  assert.deepEqual(
    fake.calls.filter((call) => call === 'unpublish' || call === 'publish'),
    ['publish', 'unpublish'],
  )
  assert.equal(await room.startPublish(), 'published')
  assert.equal(fake.mics, 1)
  await room.leave()
  assert.equal(fake.destroyed, 1)
  assert.ok(fake.calls.includes('leave'))
  assert.equal(
    fake.calls.filter((call) => call === 'unpublish').length,
    2,
  )
})

test('release before publish finishes does not leave the mic up', async () => {
  let releasePublish: () => void = () => {}
  const gate = new Promise<void>((resolve) => {
    releasePublish = resolve
  })
  let entered: () => void = () => {}
  const enteredGate = new Promise<void>((resolve) => {
    entered = resolve
  })
  const fake = new FakeTransport({
    publish: async () => {
      entered()
      await gate
    },
  })
  const room = new RtnRoom(fake)
  await room.join('room-token')
  const pending = room.startPublish()
  await enteredGate
  const stopped = room.stopPublish()
  releasePublish()
  assert.equal(await pending, 'cancelled')
  await stopped
  assert.equal(
    fake.calls.filter((call) => call === 'publish' || call === 'unpublish').join(','),
    'publish,unpublish',
  )
  assert.equal(fake.published, false)
})

test('release during microphone capture destroys the track and does not publish', async () => {
  let releaseMic: (mic: LocalMic) => void = () => {}
  let micCalled: () => void = () => {}
  const micCalledGate = new Promise<void>((resolve) => {
    micCalled = resolve
  })
  const fake = new FakeTransport({
    createMic() {
      micCalled()
      return new Promise((resolve) => {
        releaseMic = resolve
      })
    },
  })
  const room = new RtnRoom(fake)
  await room.join('room-token')
  const pending = room.startPublish()
  await micCalledGate
  const stopped = room.stopPublish()
  let destroyed = false
  releaseMic({
    destroy() {
      destroyed = true
    },
  })
  assert.equal(await pending, 'cancelled')
  await stopped
  assert.equal(destroyed, true)
  assert.equal(fake.calls.includes('publish'), false)
})

test('start before join fails and does not publish', async () => {
  const fake = new FakeTransport()
  const room = new RtnRoom(fake)
  await assert.rejects(room.startPublish(), /还没有进入频道/)
  assert.equal(fake.calls.includes('publish'), false)
})

test('duplicate remote audio is subscribed once', async () => {
  const fake = new FakeTransport()
  const room = new RtnRoom(fake)
  await room.join('room-token')
  await fake.events.onPublished(fake.conn, 'driver-2', [track('a1', 'driver-2', true)])
  assert.deepEqual(fake.subscribed, [['a1']])
})

class FakeTransport implements RtnTransport {
  calls: string[] = []
  subscribed: string[][] = []
  played: string[] = []
  stopped: string[] = []
  mics = 0
  destroyed = 0
  published = false
  events!: RtnEvents
  conn!: RtnConnection

  hooks: {
    publish?: () => Promise<void>
    createMic?: () => Promise<LocalMic>
  }

  constructor(hooks: {
    publish?: () => Promise<void>
    createMic?: () => Promise<LocalMic>
  } = {}) {
    this.hooks = hooks
  }

  async join(token: string, events: RtnEvents): Promise<RtnConnection> {
    this.calls.push('join')
    assert.equal(token, 'room-token')
    this.events = events
    const conn: RtnConnection = {
      publish: async () => {
        this.calls.push('publish')
        this.published = true
        if (this.hooks.publish) {
          await this.hooks.publish()
        }
      },
      unpublish: async () => {
        this.calls.push('unpublish')
        this.published = false
      },
      subscribe: async (tracks) => {
        this.subscribed.push(tracks.map((item) => item.id))
        assert.equal(tracks.every((item) => item.audio), true)
        return tracks.map((item) => this.audio(item))
      },
      leave: async () => {
        this.calls.push('leave')
      },
    }
    this.conn = conn
    await events.onPublished(conn, 'driver-2', [
      track('a1', 'driver-2', true),
      track('v1', 'driver-2', false),
    ])
    return conn
  }

  createMic(): Promise<LocalMic> {
    if (this.hooks.createMic) {
      return this.hooks.createMic()
    }
    this.mics += 1
    return Promise.resolve({
      destroy: () => {
        this.destroyed += 1
      },
    })
  }

  private audio(item: RemoteTrack): RemoteAudio {
    return {
      id: item.id,
      userId: item.userId,
      play: () => {
        this.played.push(item.id)
        return {
          stop: () => {
            this.stopped.push(item.id)
          },
        }
      },
    }
  }
}

function track(id: string, userId: string, audio: boolean): RemoteTrack {
  return { id, userId, audio }
}
