import { useState } from 'react'
import { api } from '../api'
import type { DemoAction } from './demo'

export interface DemoActionCopy {
  label: string
  busy: string
  done: string
  run: (token: string) => Promise<unknown>
}

export const DEMO_ACTIONS: Record<DemoAction, DemoActionCopy> = {
  close: {
    label: 'Close reservations now',
    busy: 'Closing reservations…',
    done: 'Reservations closed. The outcome appears above in a few seconds.',
    run: (token) => api.demoCloseNow(token),
  },
  start: {
    label: 'Start the class now',
    busy: 'Starting the class…',
    done: 'The class has started. Your payout is on its way.',
    run: (token) => api.demoStartClass(token),
  },
}

export interface DemoActionResult {
  ok: boolean
  text: string
  action: DemoAction
}

export function useDemoAction(token: string, onChange: () => Promise<unknown>) {
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<DemoActionResult | null>(null)

  async function run(action: DemoAction) {
    setBusy(true)
    setResult(null)
    try {
      await DEMO_ACTIONS[action].run(token)
      setResult({ ok: true, text: DEMO_ACTIONS[action].done, action })
      await onChange()
    } catch (e) {
      setResult({ ok: false, text: e instanceof Error ? e.message : 'The demo control did not apply.', action })
    } finally {
      setBusy(false)
    }
  }

  return { busy, result, run }
}
