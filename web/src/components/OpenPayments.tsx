import { useEffect, useRef, useState } from 'react'
import { api, type CandidateDate, type Workshop } from '../api'
import { lockedTerms } from '../lib/terms'
import { formatDateRange } from '../lib/time'
import { TermsList } from './Terms'
import { Button, Notice } from './ui'

interface Props {
  token: string
  date: CandidateDate
  workshop: Workshop
  onOpened: () => Promise<unknown>
}

export default function OpenPayments({ token, date, workshop, onOpened }: Props) {
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const heading = useRef<HTMLHeadingElement>(null)
  const toggle = useRef<HTMLButtonElement>(null)
  const returnFocus = useRef(false)
  const panelId = `open-${date.id}`
  const headingId = `open-${date.id}-title`

  useEffect(() => {
    if (confirming) heading.current?.focus()
    else if (returnFocus.current) toggle.current?.focus()
    returnFocus.current = false
  }, [confirming])

  function close() {
    returnFocus.current = true
    setConfirming(false)
  }

  async function open() {
    setBusy(true)
    setError(null)
    try {
      await api.openProposal(token, date.id)
      await onOpened()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Reservations could not be opened.')
      setBusy(false)
    }
  }

  return (
    <div className="flex flex-col items-start gap-3">
      <Button ref={toggle} onClick={() => (confirming ? close() : setConfirming(true))} aria-expanded={confirming} aria-controls={panelId}>
        Open reservations for this date
      </Button>
      <div
        id={panelId}
        role="region"
        aria-labelledby={headingId}
        hidden={!confirming}
        className="flex w-full flex-col gap-4 rounded-sm border border-ink bg-sheet p-4 sm:p-5"
      >
        <h4 id={headingId} ref={heading} tabIndex={-1} className="font-bold">
          These terms lock when reservations open. They cannot be changed afterwards.
        </h4>
        <TermsList
          items={lockedTerms({
            timeZone: workshop.timezone,
            dateLabel: formatDateRange(date.starts_at, date.ends_at, workshop.timezone),
            price: workshop.price,
            windowMin: workshop.payment_window_min,
            minimum: workshop.minimum,
            capacity: workshop.capacity,
            closesAbout: new Date(Date.now() + workshop.payment_window_min * 60000).toISOString(),
          })}
        />
        <p className="text-sm text-ink-soft">
          Each person reserves with a PayPal hold. Holds are charged once enough people have reserved; if not enough people pay by the
          deadline, the workshop is cancelled, holds are released and any charge is refunded.
        </p>
        <div className="flex flex-wrap gap-3">
          <Button onClick={open} busy={busy} busyLabel="Opening reservations…">
            Open reservations
          </Button>
          <Button variant="secondary" onClick={close} disabled={busy}>
            Not yet
          </Button>
        </div>
        {error && (
          <Notice tone="attention" role="alert">
            {error}
          </Notice>
        )}
      </div>
    </div>
  )
}
