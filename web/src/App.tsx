import { DeviceShell } from './device/DeviceShell.tsx'
import { useDeviceController } from './device/useDeviceController.ts'
import { ChannelPage } from './pages/ChannelPage.tsx'
import { NicknamePage } from './pages/NicknamePage.tsx'

export default function App() {
  const control = useDeviceController()
  const { device, session } = control
  const screen = !device.powered ? <p>已关机</p> : device.phase === 'booting' ? <p>启动中…</p> : !session || control.editingNickname ? (
    <NicknamePage onEnter={control.enter} joinRequest={control.joinRequest} />
  ) : (
    <ChannelPage
      feedback={control.feedback}
      onNicknameUpdated={control.setDisplayNickname}
      session={session}
      onChangeNickname={control.changeNickname}
      joinRequest={control.joinRequest}
      deviceControls={control.controls}
      onDeviceEvent={control.reportDevice}
    />
  )
  return (
    <div
      onPointerDownCapture={control.unlockSound}
      onKeyDownCapture={control.unlockSound}
      onPointerUpCapture={control.releaseSoundGesture}
      onPointerCancelCapture={control.releaseSoundGesture}
      onKeyUpCapture={control.releaseSoundGesture}
    >
      <DeviceShell status={control.status} phase={device.phase} indicator={device.indicator} onAction={control.onDeviceAction}>
        {device.powered && control.soundError ? <p className="hint" role="status">{control.soundError}</p> : null}
        {screen}
      </DeviceShell>
    </div>
  )
}
