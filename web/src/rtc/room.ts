// 进房只订阅。startPublish 才采集并推麦克风，stopPublish / leave 会停上行。
export type PublishResult = 'published' | 'cancelled'

// QN wraps the browser's NotAllowedError as SDK error 21007.
export function isPlaybackBlocked(error: unknown): boolean {
  if (!error || typeof error !== 'object') return false
  return ('name' in error && error.name === 'NotAllowedError') || ('code' in error && error.code === 21007)
}

export type RemoteTrack = {
  id: string
  userId: string
  audio: boolean
}

export type RemoteAudio = {
  id: string
  userId: string
  play(): { stop(): void; setVolume?(volume: number): void }
}

export type LocalMic = {
  setMuted(muted: boolean): void
  destroy(): void
}

export type RtnConnection = {
  publish(mic: LocalMic): Promise<void>
  unpublish(mic: LocalMic): Promise<void>
  subscribe(tracks: RemoteTrack[]): Promise<RemoteAudio[]>
  leave(): Promise<void>
}

export type RtnEvents = {
  onPublished: (
    conn: RtnConnection,
    userId: string,
    tracks: RemoteTrack[],
  ) => void | Promise<void>
  onUnpublished: (conn: RtnConnection, userId: string, tracks: RemoteTrack[]) => void
}

export type RtnTransport = {
  join(token: string, events: RtnEvents, signal: AbortSignal): Promise<RtnConnection>
  createMic(): Promise<LocalMic>
}

export class RtnRoom {
  private tail: Promise<unknown> = Promise.resolve()
  private conn: RtnConnection | null = null
  private lifetime = new AbortController()
  private mic: LocalMic | null = null
  private published = false
  private want = false
  private closed = false
  private generation = 0
  private volume = 1
  private playing = new Map<string, { stop(): void; setVolume?(volume: number): void }>()
  private pendingAudio = new Map<string, object>()
  private speakerTracks = new Map<string, Set<string>>()

  private readonly transport: RtnTransport
  private readonly onSpeakers: (userIds: string[]) => void

  constructor(transport: RtnTransport, onSpeakers: (userIds: string[]) => void = () => {}) {
    this.transport = transport
    this.onSpeakers = onSpeakers
  }

  join(token: string): Promise<void> {
    return this.enqueue(async () => {
      if (this.closed || this.conn) {
        return
      }
      const conn = await this.transport.join(token, {
        onPublished: (live, userId, tracks) => this.handlePublished(live, userId, tracks),
        onUnpublished: (live, userId, tracks) => this.handleUnpublished(live, userId, tracks),
      }, this.lifetime.signal)
      if (this.closed) {
        await conn.leave()
        return
      }
      this.conn = conn
    })
  }

  async startPublish(): Promise<PublishResult> {
    if (this.closed) {
      throw new Error('已经离开频道')
    }
    this.want = true
    const generation = this.generation
    return this.enqueue(async () => {
      if (this.closed || !this.want || generation !== this.generation) {
        return 'cancelled'
      }
      if (!this.conn) {
        throw new Error('还没有进入频道')
      }
      if (this.published) {
        return 'published'
      }
      if (!this.mic) {
        const mic = await this.transport.createMic()
        if (this.closed || !this.want || generation !== this.generation) {
          mic.destroy()
          return 'cancelled'
        }
        this.mic = mic
      }
      if (!this.want || !this.conn) {
        return 'cancelled'
      }
      const mic = this.mic
      const conn = this.conn
      mic.setMuted(false)
      try {
        await conn.publish(mic)
      } catch (error) {
        if (this.mic === mic) {
          mic.setMuted(true)
          mic.destroy()
          this.mic = null
        }
        if (this.closed) return 'cancelled'
        throw error
      }
      // leave() already destroyed the mic and closed the connection.
      if (this.closed) return 'cancelled'
      if (!this.want || generation !== this.generation) {
        await conn.unpublish(mic)
        this.published = false
        return 'cancelled'
      }
      this.published = true
      return 'published'
    })
  }

  stopPublish(): Promise<void> {
    this.want = false
    this.generation += 1
    this.mic?.setMuted(true)
    return this.enqueue(async () => {
      if (this.published && this.mic && this.conn) {
        await this.conn.unpublish(this.mic)
        this.published = false
      }
    })
  }

  leave(): Promise<void> {
    this.closed = true
    this.lifetime.abort()
    this.want = false
    this.generation += 1
    // Local capture/playback must not wait behind an unresolved SDK publish.
    if (this.mic) {
      this.mic.setMuted(true)
      this.mic.destroy()
      this.mic = null
    }
    this.published = false
    for (const playing of this.playing.values()) playing.stop()
    this.playing.clear()
    this.pendingAudio.clear()
    this.speakerTracks.clear()
    this.onSpeakers([])
    const conn = this.conn
    this.conn = null
    // The abort signal also closes a connection that is still joining.
    return conn?.leave() ?? Promise.resolve()
  }

  setVolume(volume: number): void {
    this.volume = volume
    for (const playback of this.playing.values()) playback.setVolume?.(volume)
  }

  private async handlePublished(conn: RtnConnection, userId: string, tracks: RemoteTrack[]) {
    if (this.closed) {
      return
    }
    const fresh = tracks.filter(
      (track) =>
        track.audio &&
        track.id &&
        !this.playing.has(track.id) &&
        !this.pendingAudio.has(track.id),
    )
    if (fresh.length === 0) {
      return
    }
    const subscription = {}
    for (const track of fresh) this.pendingAudio.set(track.id, subscription)
    let audio: RemoteAudio[]
    try {
      audio = await conn.subscribe(fresh)
    } catch (error) {
      for (const track of fresh) {
        if (this.pendingAudio.get(track.id) === subscription) this.pendingAudio.delete(track.id)
      }
      throw error
    }
    if (this.closed) return
    let changed = false
    for (const track of audio) {
      if (!track.id || this.pendingAudio.get(track.id) !== subscription || this.playing.has(track.id)) {
        continue
      }
      const playback = track.play()
      playback.setVolume?.(this.volume)
      this.playing.set(track.id, playback)
      const ids = this.speakerTracks.get(track.userId || userId) ?? new Set<string>()
      ids.add(track.id)
      this.speakerTracks.set(track.userId || userId, ids)
      changed = true
    }
    for (const track of fresh) {
      if (this.pendingAudio.get(track.id) === subscription) this.pendingAudio.delete(track.id)
    }
    if (changed) {
      this.emitSpeakers()
    }
  }

  private handleUnpublished(_conn: RtnConnection, userId: string, tracks: RemoteTrack[]) {
    let changed = false
    for (const track of tracks) {
      this.pendingAudio.delete(track.id)
      const playing = this.playing.get(track.id)
      if (!playing) {
        continue
      }
      playing.stop()
      this.playing.delete(track.id)
      const owner = track.userId || userId
      this.speakerTracks.get(owner)?.delete(track.id)
      changed = true
    }
    for (const [id, tracksForUser] of this.speakerTracks) {
      if (tracksForUser.size === 0) {
        this.speakerTracks.delete(id)
      }
    }
    if (changed) {
      this.emitSpeakers()
    }
  }

  private emitSpeakers() {
    this.onSpeakers([...this.speakerTracks.keys()].sort())
  }

  private enqueue<T>(task: () => Promise<T>): Promise<T> {
    const run = this.tail.then(task, task)
    this.tail = run.then(
      () => undefined,
      () => undefined,
    )
    return run
  }
}
