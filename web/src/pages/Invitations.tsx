import { useEffect, useState } from 'react'
import { inviteMember, onlineMembers, pendingInvitations, respondInvitation, type Invitation, type OnlineMember } from '../state/invitations.ts'
import type { Session } from '../state/session.ts'
import type { SignalEvent } from '../signaling/index.ts'

export function Invitations({ session, channelName, subscribe, stopUplink }: {
  session: Session
  channelName: string
  subscribe: (listener: (event: SignalEvent) => void) => () => void
  stopUplink: () => Promise<void>
}) {
  const [members, setMembers] = useState<OnlineMember[]>([])
  const [invitations, setInvitations] = useState<Invitation[]>([])
  const [name, setName] = useState(channelName || '')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const [revision, setRevision] = useState(0)
  useEffect(() => {
    let disposed = false
    void Promise.all([onlineMembers(session), pendingInvitations(session)]).then(([people, invites]) => {
      if (!disposed) { setMembers(people); setInvitations(invites) }
    }).catch((error: unknown) => { if (!disposed) setNotice(error instanceof Error ? error.message : '读取在线成员失败') })
    return () => { disposed = true }
  }, [session, revision])
  useEffect(() => subscribe(event => {
    if (event.type === 'invitation') {
      setInvitations(current => [...current.filter(item => item.id !== event.invitation.id), event.invitation])
      if (event.invitation.sender_id === session.user_id && event.invitation.status !== 'pending') {
        setNotice(event.invitation.status === 'accepted' ? '对方已接受邀请' : event.invitation.status === 'rejected' ? '对方已拒绝邀请' : '邀请已过期或未送达')
      }
    }
    if (event.type === 'disconnected') setMembers([])
  }), [subscribe, session.user_id])
  async function run(task: () => Promise<void>, message: string) {
    setBusy(true)
    setNotice('')
    try { await task(); setNotice(message) }
    catch (error) { setNotice(error instanceof Error ? error.message : '邀请操作失败') }
    finally { setBusy(false); setRevision(value => value + 1) }
  }
  return <section aria-label="在线成员与邀请" id="invitations">
    {invitations.filter(item => item.recipient_id === session.user_id && item.status === 'pending').map(item => <div key={item.id}>
      <p>{item.sender_name}邀请你加入「{item.channel_name}」</p>
      <button disabled={busy} onClick={() => void run(async () => { await stopUplink(); await respondInvitation(session, item.id, true) }, '已接受邀请')}>接受</button>
      <button disabled={busy} onClick={() => void run(() => respondInvitation(session, item.id, false), '已拒绝邀请')}>拒绝</button>
    </div>)}
    <details className="screen-menu">
    <summary>联系人与邀请 <span>查看在线成员</span></summary>
    <p className="hint">可邀请其他频道的在线成员。在线不代表正在收听你的频道。</p>
    <button disabled={busy} onClick={() => setRevision(value => value + 1)}>刷新在线成员</button>
    <label>邀请到频道<input value={name} onChange={event => setName(event.target.value)} placeholder="例如：装货" maxLength={32} /></label>
    {members.length === 0 ? <p>暂无其他在线成员</p> : <ul>{members.map(member => <li key={member.user_id}>
      {member.display_name} <small>（{member.user_id.slice(0, 8)}）</small>
      <button disabled={busy || !name.trim()} onClick={() => void run(async () => { await stopUplink(); await inviteMember(session, member.user_id, name.trim()) }, '邀请已送达，等待对方接受')}>邀请</button>
    </li>)}</ul>}
    </details>
    {notice ? <p role="status">{notice}</p> : null}
  </section>
}
