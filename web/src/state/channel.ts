import { authHeaders, type Session } from './session.ts'

export type Channel = {
  channel_id: string
  name: string
  join_code: string
  role: string
}

type ErrorBody = {
  error?: string
}

export async function createChannel(session: Session, name: string): Promise<Channel> {
  return postChannel(session, '/api/v1/channels', { name })
}

export async function joinChannelByCode(session: Session, code: string): Promise<Channel> {
  return postChannel(session, '/api/v1/channels/join', { code })
}

export async function listChannels(session: Session): Promise<Channel[]> {
  const res = await fetch('/api/v1/channels', { headers: authHeaders(session) })
  const body = await readBody(res)
  const channels = readChannelList(body)
  if (!res.ok || !channels) {
    throw new Error(errorText(body, '读取频道列表失败'))
  }
  return channels
}

export async function switchCurrentChannel(session: Session, channelID: string): Promise<Channel> {
  return postChannel(session, '/api/v1/channels/current', { channel_id: channelID })
}

// 先停上行，再改当前频道，然后离开旧房间。调用方接着只订阅进入新房间。
export async function switchListeningChannel(
  stopUplink: () => Promise<void>,
  switchCurrent: () => Promise<Channel>,
  leaveRoom: () => Promise<void>,
): Promise<Channel> {
  await stopUplink()
  const next = await switchCurrent()
  await leaveRoom()
  return next
}

export function readChannelList(value: unknown): Channel[] | null {
  if (!value || typeof value !== 'object') {
    return null
  }
  const raw = (value as { channels?: unknown }).channels
  if (!Array.isArray(raw)) {
    return null
  }
  const out: Channel[] = []
  for (const item of raw) {
    const channel = readChannel(item)
    if (!channel) {
      return null
    }
    out.push(channel)
  }
  return out
}

async function postChannel(
  session: Session,
  path: string,
  payload: { name: string } | { code: string } | { channel_id: string },
): Promise<Channel> {
  const headers = authHeaders(session)
  headers.set('Content-Type', 'application/json')
  const res = await fetch(path, {
    method: 'POST',
    headers,
    body: JSON.stringify(payload),
  })
  const body = await readBody(res)
  const channel = readChannel(body)
  if (!res.ok || !channel) {
    throw new Error(errorText(body, '操作失败'))
  }
  return channel
}

function readChannel(value: unknown): Channel | null {
  if (!value || typeof value !== 'object') {
    return null
  }
  const channel = value as Partial<Channel>
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

function errorText(value: unknown, fallback: string): string {
  if (!value || typeof value !== 'object') {
    return fallback
  }
  const message = (value as ErrorBody).error
  if (!message) {
    return fallback
  }
  return message
}

async function readBody(res: Response): Promise<unknown> {
  try {
    return await res.json()
  } catch {
    return null
  }
}
