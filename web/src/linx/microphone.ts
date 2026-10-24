// Both the voice bridge and RTN use this capture. RTN publishes a clone so SDK
// mute/destroy operations never stop the continuous speech-recognition input.
let capture: Promise<MediaStream> | null = null
let leases = 0
let playbackBlocked = false
const playbackListeners = new Set<(blocked: boolean) => void>()

export async function acquireMicrophone(): Promise<{ stream: MediaStream; release: () => void }> {
  leases += 1
  let stream: MediaStream
  try {
    capture ??= navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true } })
    stream = await capture
  } catch (error) {
    leases -= 1
    if (leases === 0) capture = null
    if (error instanceof DOMException && error.name === 'NotAllowedError') {
      throw new Error('麦克风权限未允许，请在浏览器的网站权限中允许麦克风，再按指令键重试')
    }
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
