import { useCallback, useEffect, useRef, useState } from 'react'
import type { Session } from '../state/session.ts'
import { openSignaling, reduceRosters, type Rosters, type SignalEvent, type ActionExecutor } from './index.ts'

type SignalHandlers = {
  executeAction: ActionExecutor
  onChannelCreated: (event: Extract<SignalEvent, { type: 'channel_created' }>) => void
  onChannelSwitched: (event: Extract<SignalEvent, { type: 'channel_switched' }>) => void
  onError: (message: string) => void
  onNotice: (message: string) => void
}

export function useChannelSignaling(session: Session, handlers: SignalHandlers) {
  const [rosters, setRosters] = useState<Rosters>({})
  const listeners = useRef(new Set<(event: SignalEvent) => void>())
  const handlersRef = useRef(handlers)

  useEffect(() => {
    handlersRef.current = handlers
  }, [handlers])

  useEffect(() => {
    return openSignaling(session, (event) => {
      setRosters((current) => reduceRosters(current, event))
      if (event.type === 'channel_created') {
        handlersRef.current.onChannelCreated(event)
      } else if (event.type === 'channel_switched' || event.type === 'channel_state') {
        handlersRef.current.onChannelSwitched({ ...event, type: 'channel_switched' })
      } else if (event.type === 'error') {
        handlersRef.current.onError(event.message)
      } else if (event.type === 'notice') {
        handlersRef.current.onNotice(event.message)
      }
      for (const listener of listeners.current) {
        listener(event)
      }
    }, (action, signal) => handlersRef.current.executeAction(action, signal))
  }, [session])

  const subscribe = useCallback((listener: (event: SignalEvent) => void) => {
    listeners.current.add(listener)
    return () => {
      listeners.current.delete(listener)
    }
  }, [])

  return { rosters, subscribe }
}
