import type { QNMicrophoneAudioTrack, QNRemoteAudioTrack, QNRemoteVideoTrack } from 'qnweb-rtc'
import {
  RtnRoom,
  type LocalMic,
  type RemoteAudio,
  type RemoteTrack,
  type RtnConnection,
  type RtnTransport,
} from './room.ts'

type SdkRemoteTrack = QNRemoteAudioTrack | QNRemoteVideoTrack

async function loadSdk() {
  const mod = await import('qnweb-rtc')
  return mod.default
}

export function createQiniuRoom(
  onSpeakers: (userIds: string[]) => void,
  sink: HTMLElement,
  onPlayError: (error: unknown) => void,
): RtnRoom {
  return new RtnRoom(createQiniuTransport(sink, onPlayError), onSpeakers)
}

function createQiniuTransport(sink: HTMLElement, onPlayError: (error: unknown) => void): RtnTransport {
  const mics = new WeakMap<LocalMic, QNMicrophoneAudioTrack>()
  return {
    async join(token, events) {
      const QNRTC = await loadSdk()
      const client = QNRTC.createClient()
      const byID = new Map<string, SdkRemoteTrack>()
      const conn: RtnConnection = {
        async publish(mic) {
          const track = mics.get(mic)
          if (!track) {
            throw new Error('麦克风无效')
          }
          await client.publish(track)
        },
        async unpublish(mic) {
          const track = mics.get(mic)
          if (!track) {
            return
          }
          await client.unpublish(track)
        },
        async subscribe(tracks) {
          const sdkTracks = tracks
            .map((track) => byID.get(track.id))
            .filter((track): track is SdkRemoteTrack => track != null && track.isAudio())
          if (sdkTracks.length === 0) {
            return []
          }
          const { audioTracks } = await client.subscribe(sdkTracks)
          return audioTracks.map((track) => toRemoteAudio(track, sink, onPlayError))
        },
        async leave() {
          await client.leave()
        },
      }

      const remember = (tracks: SdkRemoteTrack[]) => {
        for (const track of tracks) {
          if (track.trackID) {
            byID.set(track.trackID, track)
          }
        }
      }
      const handlePublished = async (userID: string, tracks: SdkRemoteTrack[]) => {
        remember(tracks)
        await events.onPublished(conn, userID, tracks.map(toRemoteTrack))
      }

      client.on('user-published', (userID, tracks) => {
        void handlePublished(userID, tracks).catch((err: unknown) => {
          onPlayError(err)
        })
      })
      client.on('user-unpublished', (userID, tracks) => {
        events.onUnpublished(conn, userID, tracks.map(toRemoteTrack))
        for (const track of tracks) {
          if (track.trackID) {
            byID.delete(track.trackID)
          }
        }
      })

      try {
        await client.join(token)
      } catch (error) {
        await client.leave().catch(() => {})
        throw error
      }
      for (const user of client.remoteUsers) {
        const pending = user.getAudioTracks().filter((track) => track.trackID && !track.isSubscribed())
        if (pending.length > 0) {
          await handlePublished(user.userID, pending).catch((err: unknown) => {
            onPlayError(err)
          })
        }
      }
      return conn
    },
    async createMic() {
      const QNRTC = await loadSdk()
      let track: QNMicrophoneAudioTrack
      try {
        track = await QNRTC.createMicrophoneAudioTrack()
      } catch (err) {
        const detail = err instanceof Error ? err.message : '麦克风不可用'
        throw new Error(`无法打开麦克风：${detail}`)
      }
      const mic: LocalMic = {
        setMuted(muted) {
          track.setMuted(muted)
        },
        destroy() {
          track.destroy()
        },
      }
      mics.set(mic, track)
      return mic
    },
  }
}

function toRemoteTrack(track: SdkRemoteTrack): RemoteTrack {
  return {
    id: track.trackID ?? '',
    userId: track.userID ?? '',
    audio: track.isAudio(),
  }
}

function toRemoteAudio(
  track: QNRemoteAudioTrack,
  sink: HTMLElement,
  onPlayError: (error: unknown) => void,
): RemoteAudio {
  return {
    id: track.trackID ?? '',
    userId: track.userID ?? '',
    play() {
      const holder = document.createElement('div')
      sink.append(holder)
      let stopped = false
      const stop = () => {
        stopped = true
        const media = track.mediaElement
        if (media) {
          media.pause()
          media.srcObject = null
        }
        holder.remove()
      }
      void track.play(holder).then(() => {
        if (stopped) stop()
      }, (err: unknown) => {
        if (!stopped) onPlayError(err)
      })
      return {
        setVolume(volume: number) { track.setVolume(volume) },
        stop,
      }
    },
  }
}
