import { acquireMicrophone, isPlaybackBlocked, onPlaybackBlocked } from '../linx/microphone.ts'
import type { QNCustomAudioTrack, QNRemoteAudioTrack, QNRemoteVideoTrack } from 'qnweb-rtc'
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
  const mics = new WeakMap<LocalMic, QNCustomAudioTrack>()
  return {
    async join(token, events, signal) {
      const QNRTC = await loadSdk()
      signal.throwIfAborted()
      const client = QNRTC.createClient()
      const abort = () => { void client.leave().catch(() => {}) }
      signal.addEventListener('abort', abort, { once: true })
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
          signal.removeEventListener('abort', abort)
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
        if (signal.aborted) return
        remember(tracks)
        await events.onPublished(conn, userID, tracks.map(toRemoteTrack))
      }

      client.on('user-published', (userID, tracks) => {
        void handlePublished(userID, tracks).catch((err: unknown) => {
          if (!signal.aborted) onPlayError(err)
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
        signal.throwIfAborted()
      } catch (error) {
        signal.removeEventListener('abort', abort)
        await client.leave().catch(() => {})
        throw error
      }
      for (const user of client.remoteUsers) {
        const pending = user.getAudioTracks().filter((track) => track.trackID && !track.isSubscribed())
        if (pending.length > 0) {
          await handlePublished(user.userID, pending).catch((err: unknown) => {
            if (!signal.aborted) onPlayError(err)
          })
        }
      }
      signal.removeEventListener('abort', abort)
      return conn
    },
    async createMic() {
      const QNRTC = await loadSdk()
      const lease = await acquireMicrophone()
      const input = lease.stream.getAudioTracks()[0].clone()
      let track: QNCustomAudioTrack
      try {
        track = QNRTC.createCustomAudioTrack({ mediaStreamTrack: input })
      } catch (error) {
        input.stop()
        lease.release()
        throw error
      }
      let muted = true
      const applyMute = () => {
        const blocked = muted || isPlaybackBlocked()
        // Native muting also works before publish and while unpublish is pending.
        // QN setMuted requires a published track; PTT owns publish/unpublish separately.
        input.enabled = !blocked
      }
      const unsubscribe = onPlaybackBlocked(applyMute)
      applyMute()
      const mic: LocalMic = {
        setMuted(value) {
          muted = value
          applyMute()
        },
        destroy() {
          unsubscribe()
          track.destroy()
          input.stop()
          lease.release()
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
