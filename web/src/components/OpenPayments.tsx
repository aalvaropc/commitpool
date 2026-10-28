import { useState } from 'react'
import { api, type CandidateDate, type Workshop } from '../api'
import { formatMoney } from '../lib/money'
import { formatInZone, windowLabel } from '../lib/time'
import { Button, Notice } from './ui'

interface Props {
  token: string
  date: CandidateDate
  workshop: Workshop
  onOpened: () => Promise<unknown>
}

/**
 * "Open payments for this date", then the terms that lock with it and a
 * second, explicit confirmation.
 */
export default function OpenPayments({ token, date, workshop, onOpened }: Props) {
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const panelId = `open-${date.id}`
  const closesAbout = new Date(Date.now() + workshop.payment_window_min * 60000).toISOString()

  async function open() {
    setBusy(true)
    setError(null)
    try {
      await api.openProposal(token, date.id)
      await onOpened()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Payments could not be opened.')
      setBusy(false)
    }
  }

  if (!confirming) {
    return (
      <Button onClick={() => setConfirming(true)} aria-expanded={false} aria-controls={panelId}>
        Open payments for this date
      </Button>
    )
  }

  return (
    <div id={panelId} className="flex flex-col gap-4 rounded-sm border border-ink bg-sheet p-4 sm:p-5">
      <p className="font-semibold">These terms lock when payments open. They cannot be changed afterwards.</p>
      <dl className="grid gap-x-6 gap-y-3 sm:grid-cols-2">
        <div className="sm:col-span-2">
          <dt className="text-sm text-ink-soft">Date</dt>
          <dd className="font-semibold">{date.label}</dd>
        </div>
        <div>
          <dt className="text-sm text-ink-soft">Price per seat</dt>
          <dd className="wide text-lg font-semibold tabular-nums">{formatMoney(workshop.price)}</dd>
        </div>
        <div>
          <dt className="text-sm text-ink-soft">Payment window</dt>
          <dd className="font-semibold">{windowLabel(workshop.payment_window_min)}</dd>
        </div>
        <div>
          <dt className="text-sm text-ink-soft">Minimum paid seats</dt>
          <dd className="semiwide text-lg font-semibold tabular-nums">{workshop.minimum}</dd>
        </div>
        <div>
          <dt className="text-sm text-ink-soft">Capacity</dt>
          <dd className="semiwide text-lg font-semibold tabular-nums">{workshop.capacity}</dd>
        </div>
        <div className="sm:col-span-2">
          <dt className="text-sm text-ink-soft">Payments close about</dt>
          <dd className="font-semibold">{formatInZone(closesAbout, workshop.timezone)}</dd>
        </div>
      </dl>
      <p className="text-sm text-ink-soft">
        Participants authorize a hold. Holds are charged once enough of them can reach the minimum; if the minimum of paid seats is
        not reached by the deadline, the workshop is cancelled, holds are released and charges refunded.
      </p>
      <div className="flex flex-wrap gap-3">
        <Button onClick={open} busy={busy} busyLabel="Opening payments…">
          Open payments now
        </Button>
        <Button variant="secondary" onClick={() => setConfirming(false)} disabled={busy}>
          Keep collecting availability
        </Button>
      </div>
      {error && (
        <Notice tone="attention" role="alert">
          {error}
        </Notice>
      )}
    </div>
  )
}
