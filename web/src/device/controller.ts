export type DevicePhase =
  | 'off'
  | 'booting'
  | 'standby'
  | 'listening'
  | 'command-listening'
  | 'processing'
  | 'confirming'
  | 'talking'
  | 'error'

export type DeviceEvent =
  | { type: 'POWER_TOGGLE' }
  | { type: 'BOOT_READY'; hasChannel: boolean }
  | { type: 'CHANNEL_READY' }
  | { type: 'COMMAND_START' }
  | { type: 'COMMAND_END' }
  | { type: 'COMMAND_PROCESSING' }
  | { type: 'COMMAND_CONFIRM' }
  | { type: 'COMMAND_CANCEL' }
  | { type: 'COMMAND_SUCCESS'; hasChannel: boolean }
  | { type: 'TALK_START' }
  | { type: 'TALK_END' }
  | { type: 'FAIL' }
  | { type: 'RESET' }
  | { type: 'MEDIA_STATE'; ready: boolean; talking: boolean; failed: boolean }

export type DeviceSnapshot = {
  phase: DevicePhase
  powered: boolean
  revision: number
  screen: string
  indicator: 'off' | 'blue' | 'amber' | 'red'
  sound: 'none' | 'boot' | 'ready' | 'confirm' | 'error'
}

export type DeviceResources = {
  release: () => void
  restore: () => void
}

export const initialDeviceSnapshot: DeviceSnapshot = {
  phase: 'off',
  powered: false,
  revision: 0,
  screen: '已关机',
  indicator: 'off',
  sound: 'none',
}

const allowed: Record<DevicePhase, Partial<Record<DeviceEvent['type'], true>>> = {
  off: { POWER_TOGGLE: true, RESET: true },
  booting: { POWER_TOGGLE: true, BOOT_READY: true, FAIL: true },
  standby: {
    POWER_TOGGLE: true,
    CHANNEL_READY: true,
    COMMAND_START: true,
    FAIL: true,
  },
  listening: {
    POWER_TOGGLE: true,
    COMMAND_START: true,
    TALK_START: true,
    FAIL: true,
  },
  'command-listening': {
    POWER_TOGGLE: true,
    COMMAND_END: true,
    COMMAND_PROCESSING: true,
    COMMAND_CANCEL: true,
    TALK_START: true,
    FAIL: true,
  },
  processing: {
    POWER_TOGGLE: true,
    COMMAND_CONFIRM: true,
    COMMAND_CANCEL: true,
    COMMAND_SUCCESS: true,
    FAIL: true,
  },
  confirming: {
    POWER_TOGGLE: true,
    COMMAND_START: true,
    COMMAND_CANCEL: true,
    COMMAND_SUCCESS: true,
    FAIL: true,
  },
  talking: {
    POWER_TOGGLE: true,
    COMMAND_START: true,
    TALK_END: true,
    FAIL: true,
  },
  error: { POWER_TOGGLE: true, RESET: true, FAIL: true },
}

export function reduceDevice(snapshot: DeviceSnapshot, event: DeviceEvent): DeviceSnapshot {
  if (event.type === 'MEDIA_STATE') {
    if (!snapshot.powered || snapshot.phase === 'booting') return snapshot
    const phase = event.failed ? 'error' : event.talking ? 'talking' : event.ready ? 'listening' : 'standby'
    if (phase === snapshot.phase) return snapshot
    return { ...snapshot, phase, revision: snapshot.revision + 1, ...feedbackFor(phase) }
  }
  if (!allowed[snapshot.phase][event.type]) return snapshot

  let phase = snapshot.phase
  let powered = snapshot.powered
  switch (event.type) {
    case 'POWER_TOGGLE':
      if (powered) {
        phase = 'off'
        powered = false
      } else {
        phase = 'booting'
        powered = true
      }
      break
    case 'BOOT_READY':
      phase = event.hasChannel ? 'listening' : 'standby'
      break
    case 'CHANNEL_READY':
      phase = 'listening'
      break
    case 'COMMAND_START':
      phase = 'command-listening'
      break
    case 'COMMAND_END':
      phase = 'processing'
      break
    case 'COMMAND_PROCESSING':
      phase = 'processing'
      break
    case 'COMMAND_CONFIRM':
      phase = 'confirming'
      break
    case 'COMMAND_CANCEL':
      phase = 'listening'
      break
    case 'COMMAND_SUCCESS':
      phase = event.hasChannel ? 'listening' : 'standby'
      break
    case 'TALK_START':
      phase = 'talking'
      break
    case 'TALK_END':
      phase = 'listening'
      break
    case 'FAIL':
      phase = 'error'
      break
    case 'RESET':
      phase = 'standby'
      powered = true
      break
  }
  const feedback = feedbackFor(phase)
  return { phase, powered, revision: snapshot.revision + 1, ...feedback }
}

function feedbackFor(phase: DevicePhase): Pick<DeviceSnapshot, 'screen' | 'indicator' | 'sound'> {
  switch (phase) {
    case 'off': return { screen: '已关机', indicator: 'off', sound: 'none' }
    case 'booting': return { screen: '启动中', indicator: 'amber', sound: 'boot' }
    case 'standby': return { screen: '待命', indicator: 'blue', sound: 'ready' }
    case 'listening': return { screen: '可收听', indicator: 'blue', sound: 'none' }
    case 'command-listening': return { screen: '请说指令', indicator: 'amber', sound: 'none' }
    case 'processing': return { screen: '处理中', indicator: 'amber', sound: 'none' }
    case 'confirming': return { screen: '等待确认', indicator: 'amber', sound: 'confirm' }
    case 'talking': return { screen: '正在发送', indicator: 'blue', sound: 'none' }
    case 'error': return { screen: '设备异常', indicator: 'red', sound: 'error' }
  }
}

export function phaseLabel(phase: DevicePhase): string {
  switch (phase) {
    case 'off': return '关机'
    case 'booting': return '启动中'
    case 'standby': return '待命'
    case 'listening': return '可收听'
    case 'command-listening': return '指令中'
    case 'processing': return '处理中'
    case 'confirming': return '等待确认'
    case 'talking': return '正在发送'
    case 'error': return '设备异常'
  }
}

export function actionEvent(
  action: 'power' | 'command' | 'ptt-start' | 'ptt-end',
  phase?: DevicePhase,
): DeviceEvent {
  switch (action) {
    case 'power': return { type: 'POWER_TOGGLE' }
    case 'command': return phase === 'command-listening' ? { type: 'COMMAND_END' } : { type: 'COMMAND_START' }
    case 'ptt-start': return { type: 'TALK_START' }
    case 'ptt-end': return { type: 'TALK_END' }
  }
}

export function canPerformAction(snapshot: DeviceSnapshot, action: 'power' | 'command' | 'ptt-start' | 'ptt-end'): boolean {
  return Boolean(allowed[snapshot.phase][actionEvent(action, snapshot.phase).type])
}

export function syncDeviceResources(
  previous: DeviceSnapshot,
  next: DeviceSnapshot,
  resources: DeviceResources,
): void {
  if (previous.phase !== 'off' && next.phase === 'off') {
    resources.release()
  } else if (previous.phase === 'off' && next.phase === 'booting') {
    resources.restore()
  }
}
