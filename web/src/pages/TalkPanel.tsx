import type { PointerEvent as ReactPointerEvent } from 'react'
import { speakingText } from '../signaling/index.ts'
import { memberIsLive } from '../state/presence.ts'
import { useDeviceTalk, type DeviceTalkProps } from '../device/useDeviceTalk.ts'

export function TalkPanel(props: DeviceTalkProps) {
  const { session, members, commanding, sendingDisabled, onCommand, commandDisabled } = props
  const {
    view,
    error,
    publishing,
    listenHint,
    sinkRef,
    joined,
    talking,
    press,
    release,
    end,
    unlockAudio,
    keyboardHoldRef,
    retry,
  } = useDeviceTalk(props)
  function onPointerDown(event: ReactPointerEvent<HTMLButtonElement>) {
    try {
      event.currentTarget.setPointerCapture(event.pointerId)
    } catch {
      // 指针已经结束时捕获会失败，松手仍由 pointerup / pointercancel 处理。
    }
    press()
  }

  return (
    <section className="talk">
      {error ? <p className="error">{error}</p> : null}
      {listenHint ? (
        <p className="hint">
          {listenHint}{' '}
          <button type="button" className="secondary" onClick={unlockAudio}>
            打开声音
          </button>
        </p>
      ) : null}
      <div ref={sinkRef} className="remote-audio" />
      {members && members.length > 0 ? (
        <ul className="roster" aria-live="polite">
          {members.map((member) => {
            const mine = member.user_id === session.user_id
            const live = memberIsLive(
              member.user_id,
              member.talking,
              session.user_id,
              talking,
            )
            const name = member.display_name.trim() || '未命名'
            return (
              <li
                className={live ? 'roster-row live' : 'roster-row'}
                key={member.user_id}
              >
                <span className="roster-name">
                  {name}
                  {mine ? '（我）' : ''}
                </span>
                {live ? (
                  <span className="roster-live">
                    {mine ? '你正在说' : speakingText(name)}
                  </span>
                ) : null}
              </li>
            )
          })}
        </ul>
      ) : (
        <p className="hint">{members ? '频道里还没有人' : '成员列表同步中'}</p>
      )}
      {view === 'error' ? (
        <button
          type="button"
          className="secondary"
          onClick={retry}
        >
          重试进房
        </button>
      ) : null}
      <div className="talk-dock">
        <div className="talk-dock-inner">
          <button
            type="button"
            className={talking ? 'ptt talking' : 'ptt'}
            disabled={!joined || commanding || sendingDisabled}
            aria-pressed={talking}
            onPointerDown={onPointerDown}
            onPointerUp={release}
            onPointerCancel={release}
            onLostPointerCapture={release}
            onKeyDown={(event) => {
              if (
                event.repeat ||
                (event.key !== ' ' && event.key !== 'Enter')
              ) {
                return
              }
              event.preventDefault()
              press(true)
            }}
            onKeyUp={(event) => {
              if (event.key !== ' ' && event.key !== 'Enter') {
                return
              }
              event.preventDefault()
              release()
            }}
            onBlur={() => {
              if (keyboardHoldRef.current) {
                release()
              }
            }}
          >
            {publishing ? '准备发送' : talking ? '松开结束' : '按住说话'}
          </button>
          <div className="dock-side">
            <button
              type="button"
              className="end"
              disabled={!joined}
              onClick={end}
            >
              结束
            </button>
            <button
              type="button"
              className={commanding ? 'command on' : 'command'}
              disabled={commandDisabled}
              aria-pressed={commanding}
              onClick={onCommand}
            >
              {commanding ? '结束指令' : '指令麦'}
            </button>
          </div>
        </div>
      </div>
    </section>
  )
}
