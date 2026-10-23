// InChannel 只收听；Talking 才上行。结束键和松手都回到 InChannel。
export type TalkPhase = 'out' | 'in_channel' | 'talking'

export type TalkEvent = 'JOINED_CHANNEL' | 'TALK_START' | 'TALK_END' | 'LEFT'

export function reduceTalk(phase: TalkPhase, event: TalkEvent): TalkPhase {
  switch (event) {
    case 'JOINED_CHANNEL':
      return phase === 'talking' ? 'talking' : 'in_channel'
    case 'TALK_START':
      return phase === 'out' ? 'out' : 'talking'
    case 'TALK_END':
      return phase === 'talking' ? 'in_channel' : phase
    case 'LEFT':
      return 'out'
  }
}
