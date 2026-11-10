import { useRef, useState } from 'react'
import type { Payment } from '../api'
import { formatAmount } from '../lib/money'
import { Button, Icon } from './ui'

function CopyId({ label, spoken, value, who }: { label: string; spoken: string; value: string; who: string }) {
  const code = useRef<HTMLElement>(null)
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle')

  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setState('copied')
    } catch {
      const node = code.current
      const selection = window.getSelection()
      if (node && selection) selection.selectAllChildren(node)
      setState('failed')
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5">
      <dt className="w-28 shrink-0 text-ink-soft">{label}</dt>
      <dd className="flex min-w-0 flex-wrap items-center gap-x-2">
        <code ref={code} className="font-mono text-sm text-ink [overflow-wrap:anywhere]">
          {value}
        </code>
        <Button variant="quiet" size="sm" onClick={() => void copy()} className="px-3">
          <Icon name="copy" size={16} />
          Copy<span className="sr-only"> {spoken} ID for {who}</span>
        </Button>
        <span role="status" className="text-xs text-ink-soft">
          {state === 'copied' ? 'Copied.' : state === 'failed' ? 'Selected; copy it with Ctrl+C or ⌘C.' : ''}
        </span>
      </dd>
    </div>
  )
}

export default function PayPalRecord({ payment, who }: { payment: Payment; who: string }) {
  const ids: [string, string, string | null][] = [
    ['PayPal order', 'PayPal order', payment.order_id],
    ['Hold', 'hold', payment.authorization_id],
    ['Charge', 'charge', payment.capture_id],
    ['Refund', 'refund', payment.refund_id],
  ]
  return (
    <dl className="mt-2 flex flex-col gap-1">
      {ids.map(([label, spoken, id]) => id && <CopyId key={label} label={label} spoken={spoken} value={id} who={who} />)}
      {payment.fee && payment.net && (
        <>
          <div className="flex flex-wrap items-center gap-x-3">
            <dt className="w-28 shrink-0 text-ink-soft">PayPal fee</dt>
            <dd className="tabular-nums">{formatAmount(payment.fee)}</dd>
          </div>
          <div className="flex flex-wrap items-center gap-x-3">
            <dt className="w-28 shrink-0 text-ink-soft">Net</dt>
            <dd className="tabular-nums">{formatAmount(payment.net)}</dd>
          </div>
        </>
      )}
    </dl>
  )
}
