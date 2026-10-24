import { authHeaders, type Session } from '../state/session.ts'
import { readInvitation, type Invitation } from '../state/invitations.ts'

export type Member = {
  user_id: string
  display_name: string
  talking: boolean
}

export type DeviceCommand = { type: 'action'; action_id: string; action: string; channel_id?: string; value?: string }
export type ActionExecutor = (action: DeviceCommand, signal: AbortSignal) => Promise<void>

export type SignalEvent =
  | DeviceCommand
  | { type: 'invitation'; invitation: Invitation }
  | { type: 'action_cancel'; action_id: string }
  | { type: 'channel_state'; channel_id: string; name: string }
  | { type: 'talk_start'; channel_id: string; user_id: string }
  | { type: 'talk_end'; channel_id: string; user_id: string }
  | { type: 'members'; channel_id: string; members: Member[] }
  | { type: 'channel_created'; channel_id: string; name: string; join_code: string }
  | { type: 'channel_switched'; channel_id: string; name: string }
  | { type: 'error'; message: string }
  | { type: 'notice'; message: string }
  | { type: 'pong' }
  | { type: 'disconnected' }

export type Rosters = Record<string, Member[]>

type ErrorBody = {
  error?: string
}

export function parseSignal(raw: string): SignalEvent | null {
  let value: unknown
  try {
    value = JSON.parse(raw)
  } catch {
    return null
  }
  if (!value || typeof value !== 'object') {
    return null
  }
  const rec = value as Record<string, unknown>
  switch (rec.type) {
    case 'invitation': {
      const invitation = readInvitation(rec.invitation)
      return invitation ? { type: 'invitation', invitation } : null
    }
    case 'action':
      if (typeof rec.action_id !== 'string' || !rec.action_id || typeof rec.action !== 'string') return null
      if (rec.channel_id !== undefined && typeof rec.channel_id !== 'string') return null
      if (rec.value !== undefined && typeof rec.value !== 'string') return null
      return { type: 'action', action_id: rec.action_id, action: rec.action, channel_id: rec.channel_id as string | undefined, value: rec.value as string | undefined }
    case 'action_cancel':
      return typeof rec.action_id === 'string' && rec.action_id ? { type: 'action_cancel', action_id: rec.action_id } : null
    case 'pong':
      return { type: 'pong' }
    case 'error':
    case 'notice':
      return typeof rec.message === 'string' ? { type: rec.type, message: rec.message } : null
    case 'talk_start':
    case 'talk_end':
      if (typeof rec.channel_id !== 'string' || typeof rec.user_id !== 'string') {
        return null
      }
      return { type: rec.type, channel_id: rec.channel_id, user_id: rec.user_id }
    case 'members':
      return parseMembers(rec)
    case 'channel_created':
      if (
        typeof rec.channel_id !== 'string' ||
        typeof rec.name !== 'string' ||
        typeof rec.join_code !== 'string'
      ) {
        return null
      }
      return {
        type: 'channel_created',
        channel_id: rec.channel_id,
        name: rec.name,
        join_code: rec.join_code,
      }
    case 'channel_state':
    case 'channel_switched':
      if (typeof rec.channel_id !== 'string' || typeof rec.name !== 'string') {
        return null
      }
      return { type: rec.type, channel_id: rec.channel_id, name: rec.name }
    default:
      return null
  }
}

export function reduceRosters(rosters: Rosters, event: SignalEvent): Rosters {
  if (event.type === 'members') {
    return { ...rosters, [event.channel_id]: event.members }
  }
  if (event.type !== 'talk_start' && event.type !== 'talk_end') {
    return rosters
  }
  const current = rosters[event.channel_id]
  if (!current) {
    return rosters
  }
  let changed = false
  const talking = event.type === 'talk_start'
  const members = current.map((member) => {
    if (member.user_id !== event.user_id || member.talking === talking) {
      return member
    }
    changed = true
    return { ...member, talking }
  })
  if (!changed) {
    return rosters
  }
  return { ...rosters, [event.channel_id]: members }
}

export function remoteSpeakers(members: readonly Member[] | undefined, selfID: string): Member[] {
  if (!members) {
    return []
  }
  return members.filter((member) => member.talking && member.user_id !== selfID)
}

export function speakingText(displayName: string): string {
  const name = displayName.trim()
  if (!name) {
    return '有人正在说'
  }
  return `${name}正在说`
}

export function selfTalkAction(
  event: SignalEvent,
  channelID: string,
  selfID: string,
): 'start' | 'end' | null {
  if (
    (event.type === 'talk_start' || event.type === 'talk_end') &&
    event.channel_id === channelID &&
    event.user_id === selfID
  ) {
    return event.type === 'talk_start' ? 'start' : 'end'
  }
  return null
}

// 本机按下 PTT 后，服务端会把 talk_start 再推回来。suppress > 0 时这是回声，不要重复 publish。
// suppress 为 0 的 talk_start 来自指令路径，本机需要开始上行。
export function observeSelfTalk(
  suppress: number,
  action: 'start' | 'end',
): { suppress: number; follow: boolean } {
  if (action === 'end') {
    return { suppress, follow: true }
  }
  if (suppress > 0) {
    return { suppress: suppress - 1, follow: false }
  }
  return { suppress, follow: true }
}

export async function startTalk(session: Session): Promise<void> {
  await postTalk(session, '/api/v1/talk/start')
}

export async function endTalk(session: Session): Promise<void> {
  await postTalk(session, '/api/v1/talk/end')
}

export function openSignaling(session: Session, onEvent: (event: SignalEvent) => void, execute?: ActionExecutor): () => void {
  let closed = false
  let socket: WebSocket | null = null
  let timer = 0
  let delay = 1000
  const actions = new Map<string, { controller: AbortController; reply?: string }>()
  const cancelActions = () => { for (const action of actions.values()) action.controller.abort(); actions.clear() }

  const connect = () => {
    if (closed) {
      return
    }
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${location.host}/api/v1/device/ws?token=${encodeURIComponent(session.session_token)}`
    const ws = new WebSocket(url)
    socket = ws
    ws.onopen = () => {
      delay = 1000
    }
    ws.onmessage = (message) => {
      const event = parseSignal(String(message.data))
      if (event?.type === 'action_cancel') {
        actions.get(event.action_id)?.controller.abort()
        return
      }
      if (event?.type === 'action') {
        const existing = actions.get(event.action_id)
        if (existing) {
          if (existing.reply && ws.readyState === WebSocket.OPEN) ws.send(existing.reply)
          return
        }
        if (actions.size >= 256) {
          const completed = [...actions.entries()].find(([, item]) => item.reply)
          if (completed) actions.delete(completed[0])
          else { ws.close(); return }
        }
        const entry: { controller: AbortController; reply?: string } = { controller: new AbortController() }
        actions.set(event.action_id, entry)
        void (async () => {
          let success = false
          let message = ''
          try {
            if (!execute) throw new Error('设备动作尚未就绪')
            await execute(event, entry.controller.signal)
            if (entry.controller.signal.aborted) throw new Error('动作已取消')
            success = true
          } catch (error) { message = error instanceof Error ? error.message : '动作失败' }
          entry.reply = JSON.stringify({ type: 'action_result', action_id: event.action_id, success, message: message.slice(0, 256) })
          if (ws.readyState === WebSocket.OPEN) ws.send(entry.reply)
        })()
        return
      }
      if (event) onEvent(event)
    }
    ws.onclose = (event) => {
      if (closed) {
        return
      }
      cancelActions()
      onEvent({ type: 'disconnected' })
      if (event.code === 4001) {
        closed = true
        window.clearInterval(ping)
        onEvent({ type: 'error', message: '设备已在另一页面连接，请关闭重复页面' })
        return
      }
      timer = window.setTimeout(connect, delay)
      delay = Math.min(delay * 2, 10000)
    }
  }

  connect()
  const ping = window.setInterval(() => {
    if (socket && socket.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: 'ping' }))
    }
  }, 20000)

  return () => {
    closed = true
    cancelActions()
    window.clearInterval(ping)
    window.clearTimeout(timer)
    socket?.close()
  }
}

function parseMembers(rec: Record<string, unknown>): SignalEvent | null {
  if (typeof rec.channel_id !== 'string' || !Array.isArray(rec.members)) {
    return null
  }
  const members: Member[] = []
  for (const item of rec.members) {
    if (!item || typeof item !== 'object') {
      return null
    }
    const member = item as Record<string, unknown>
    if (
      typeof member.user_id !== 'string' ||
      typeof member.display_name !== 'string' ||
      typeof member.talking !== 'boolean'
    ) {
      return null
    }
    members.push({
      user_id: member.user_id,
      display_name: member.display_name,
      talking: member.talking,
    })
  }
  return { type: 'members', channel_id: rec.channel_id, members }
}

async function postTalk(session: Session, path: string): Promise<void> {
  const headers = authHeaders(session)
  headers.set('Content-Type', 'application/json')
  const res = await fetch(path, { method: 'POST', headers, body: '{}' })
  if (res.ok) {
    return
  }
  let message = '对讲状态更新失败'
  try {
    const body = (await res.json()) as ErrorBody
    if (body.error) {
      message = body.error
    }
  } catch {
    // 保留上面的默认文案。
  }
  throw new Error(message)
}
