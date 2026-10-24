// Both the voice bridge and RTN use this capture. RTN publishes a clone so SDK
// mute/destroy operations never stop the continuous speech-recognition input.
let capture: Promise<MediaStream> | null = null
let leases = 0
let playbackBlocked = false
const playbackListeners = new Set<(blocked: boolean) => void>()

export async function acquireMicrophone(): Promise<{ stream: MediaStream; release: () => void }> {
  leases += 1
  capture ??= navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true } })
  let stream: MediaStream
  try {
    stream = await capture
  } catch (error) {
    leases -= 1
    if (leases === 0) capture = null
    throw error
  }
  let released = false
  return { stream, release: () => {
    if (released) return
    released = true
    leases -= 1
    if (leases === 0) {
      stream.getTracks().forEach((track) => track.stop())
      capture = null
    }
  } }
}

export function isPlaybackBlocked(): boolean { return playbackBlocked }
export function setPlaybackBlocked(blocked: boolean): void {
  if (blocked === playbackBlocked) return
  playbackBlocked = blocked
  playbackListeners.forEach((listener) => listener(blocked))
}
export function onPlaybackBlocked(listener: (blocked: boolean) => void): () => void {
  playbackListeners.add(listener)
  return () => { playbackListeners.delete(listener) }
}
