import { authHeaders, type Session } from './session.ts'

export type OnlineMember = { user_id: string; display_name: string }
export type Invitation = {
  id: string
  channel_id: string
  channel_name: string
  sender_id: string
  sender_name: string
  recipient_id: string
  expires_at: string
  status: 'pending' | 'accepted' | 'rejected' | 'expired'
}

export function readInvitation(value: unknown): Invitation | null {
  if (!value || typeof value !== 'object') return null
  const v = value as Record<string, unknown>
  if (!['id', 'channel_id', 'channel_name', 'sender_id', 'sender_name', 'recipient_id', 'expires_at'].every(key => typeof v[key] === 'string' && v[key])) return null
  if (!['pending', 'accepted', 'rejected', 'expired'].includes(String(v.status))) return null
  return v as Invitation
}

async function request<T>(session: Session, path: string, body?: object): Promise<T> {
  const headers = authHeaders(session)
  if (body) headers.set('Content-Type', 'application/json')
  const res = await fetch(path, { method: body ? 'POST' : 'GET', headers, body: body ? JSON.stringify(body) : undefined })
  const value = await res.json()
  if (!res.ok) throw new Error(value.error || '邀请操作失败')
  return value
}
export async function onlineMembers(session: Session): Promise<OnlineMember[]> {
  const result = await request<{ members: OnlineMember[] }>(session, '/api/v1/online-members')
  return result.members
}
export async function pendingInvitations(session: Session): Promise<Invitation[]> {
  const result = await request<{ invitations: unknown[] }>(session, '/api/v1/invitations')
  return result.invitations.map(readInvitation).filter((v: Invitation | null): v is Invitation => v !== null)
}
export async function inviteMember(session: Session, recipient_id: string, name: string): Promise<void> {
  await request(session, '/api/v1/invitations', { recipient_id, name })
}
export async function respondInvitation(session: Session, id: string, accept: boolean): Promise<void> {
  await request(session, `/api/v1/invitations/${encodeURIComponent(id)}/${accept ? 'accept' : 'reject'}`, {})
}
