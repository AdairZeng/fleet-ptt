import type { TalkPhase } from './talk.ts'

// 顶栏短标签与中部大字用同一套文案，余光扫视时三种主状态不会混。
export type RoomView = 'joining' | 'ready' | 'error'

export type Presence =
  | 'idle'
  | 'joining'
  | 'offline'
  | 'listening'
  | 'commanding'
  | 'talking'

export type PresenceCopy = {
  label: string
  detail: string
}

const copy: Record<Presence, PresenceCopy> = {
  idle: {
    label: '未进频道',
    detail: '可以创建频道，输入口令或扫码加入。',
  },
  joining: {
    label: '进房中',
    detail: '正在进入频道，只收听…',
  },
  offline: {
    label: '还没进房',
    detail: '',
  },
  listening: {
    label: '可听',
    detail: '',
  },
  commanding: {
    label: '指令中',
    detail: '发送已暂停，小路正在处理语音指令。',
  },
  talking: {
    label: '正在讲',
    detail: '',
  },
}

export function talkPresence(
  view: RoomView,
  talk: TalkPhase,
  commanding: boolean,
): Presence {
  if (commanding && view !== 'error') {
    return 'commanding'
  }
  if (view === 'joining') {
    return 'joining'
  }
  if (view === 'error') {
    return 'offline'
  }
  if (talk === 'talking') {
    return 'talking'
  }
  return 'listening'
}

export function idlePresence(commanding: boolean): Presence {
  return commanding ? 'commanding' : 'idle'
}

export function presenceCopy(presence: Presence): PresenceCopy {
  return copy[presence]
}

export function memberCountText(count: number | undefined): string {
  if (count === undefined) {
    return '…'
  }
  return `${count} 人`
}

export function memberIsLive(
  userID: string,
  talking: boolean,
  selfID: string,
  selfTalking: boolean,
): boolean {
  return talking || (userID === selfID && selfTalking)
}
