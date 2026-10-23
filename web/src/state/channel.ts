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

async function postChannel(
  session: Session,
  path: string,
  payload: { name: string } | { code: string },
): Promise<Channel> {
  const headers = authHeaders(session)
  headers.set('Content-Type', 'application/json')
  const res = await fetch(path, {
    method: 'POST',
    headers,
    body: JSON.stringify(payload),
  })
  const body = (await readBody(res)) as ErrorBody & Partial<Channel>
  if (!res.ok || !body.channel_id || !body.name || !body.join_code || !body.role) {
    throw new Error(body.error || '操作失败')
  }
  return {
    channel_id: body.channel_id,
    name: body.name,
    join_code: body.join_code,
    role: body.role,
  }
}

async function readBody(res: Response): Promise<ErrorBody> {
  try {
    return (await res.json()) as ErrorBody
  } catch {
    return {}
  }
}
