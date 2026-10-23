import { loadDeviceCredential, registerDevice } from './device.ts'

const sessionKey = 'fleet-ptt.session'

export type Session = {
  user_id: string
  session_token: string
  nickname: string
  device_id?: string
  linx_device_id?: string
}

export type MeChannel = {
  channel_id: string
  name: string
  join_code: string
  role: string
}

export type Me = {
  user_id: string
  nickname: string
  device_id: string
  channel: MeChannel | null
}

type ErrorBody = {
  error?: string
}

export function loadSession(): Session | null {
  const raw = localStorage.getItem(sessionKey)
  if (!raw) {
    return null
  }
  try {
    const parsed = JSON.parse(raw) as Partial<Session>
    if (!parsed.user_id || !parsed.session_token || !parsed.nickname) {
      return null
    }
    return {
      user_id: parsed.user_id,
      session_token: parsed.session_token,
      nickname: parsed.nickname,
      device_id: parsed.device_id,
      linx_device_id: parsed.linx_device_id,
    }
  } catch {
    return null
  }
}

export function saveSession(session: Session) {
  localStorage.setItem(sessionKey, JSON.stringify(session))
}

export function clearSession() {
  localStorage.removeItem(sessionKey)
}

// authHeaders 给后续 API 带上 Bearer session_token。创建会话本身不需要它。
export function authHeaders(session: Session | null): Headers {
  const headers = new Headers()
  if (session?.session_token) {
    headers.set('Authorization', `Bearer ${session.session_token}`)
  }
  return headers
}

export async function createSession(nickname: string, previous = loadSession()): Promise<Session> {
  const credential = loadDeviceCredential() ?? await registerDevice(previous?.session_token)
  const res = await fetch('/api/v1/device/session', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ nickname, ...credential }),
  })
  const body = (await readBody(res)) as ErrorBody & Partial<Session>
  if (!res.ok || !body.user_id || !body.session_token) {
    throw new Error(body.error || '进入失败')
  }
  return {
    user_id: body.user_id,
    session_token: body.session_token,
    nickname: body.nickname || nickname,
    device_id: body.device_id || credential.device_id,
    linx_device_id: body.linx_device_id || undefined,
  }
}

export async function fetchMe(session: Session): Promise<Me> {
  const res = await fetch('/api/v1/me', { headers: authHeaders(session) })
  const body = (await readBody(res)) as ErrorBody & {
    user_id?: string
    nickname?: string
    device_id?: string
    channel?: unknown
  }
  if (!res.ok || !body.user_id || !body.nickname || !body.device_id) {
    throw new Error(body.error || '身份无效')
  }
  return {
    user_id: body.user_id,
    nickname: body.nickname,
    device_id: body.device_id,
    channel: readChannel(body.channel),
  }
}

function readChannel(value: unknown): MeChannel | null {
  if (!value || typeof value !== 'object') {
    return null
  }
  const channel = value as Partial<MeChannel>
  if (!channel.channel_id || !channel.name || !channel.join_code || !channel.role) {
    return null
  }
  return {
    channel_id: channel.channel_id,
    name: channel.name,
    join_code: channel.join_code,
    role: channel.role,
  }
}

async function readBody(res: Response): Promise<ErrorBody> {
  try {
    return (await res.json()) as ErrorBody
  } catch {
    return {}
  }
}
