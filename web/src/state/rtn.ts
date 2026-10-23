import { authHeaders, type Session } from './session.ts'

export type RoomToken = {
  token: string
  app_id: string
  room_name: string
  user_id: string
  expire_at: number
}

type ErrorBody = {
  error?: string
}

export async function fetchRoomToken(session: Session, channelId: string): Promise<RoomToken> {
  const res = await fetch(`/api/v1/channels/${encodeURIComponent(channelId)}/rtn-token`, {
    headers: authHeaders(session),
  })
  const body = (await readBody(res)) as ErrorBody & Partial<RoomToken>
  if (!res.ok || !body.token || !body.room_name || !body.user_id) {
    throw new Error(body.error || '获取进房凭证失败')
  }
  return {
    token: body.token,
    app_id: body.app_id ?? '',
    room_name: body.room_name,
    user_id: body.user_id,
    expire_at: body.expire_at ?? 0,
  }
}

async function readBody(res: Response): Promise<ErrorBody> {
  try {
    return (await res.json()) as ErrorBody
  } catch {
    return {}
  }
}
