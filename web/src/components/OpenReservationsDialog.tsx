import { useState } from 'react'
import { api, type OwnerView } from '../api'
import { lockedTerms } from '../lib/terms'
import { formatDateRange, formatDay } from '../lib/time'
import { TermsList } from './Terms'
import { ConfirmDialog, Notice } from './ui'

interface Props {
  token: string
  view: OwnerView
  dateId: string | null
  onClose: () => void
  onOpened: () => Promise<unknown>
}

export default function OpenReservationsDialog({ token, view, dateId, onClose, onOpened }: Props) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const w = view.workshop
  const date = view.dates.find((d) => d.id === dateId)

  async function open() {
    if (!date) return
    setBusy(true)
    setError(null)
    try {
      await api.openProposal(token, date.id)
      onClose()
      await onOpened()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Reservations could not be opened.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <ConfirmDialog
      open={date !== undefined}
      title={date ? `Open reservations for ${formatDay(date.starts_at, w.timezone)}?` : 'Open reservations?'}
      confirmLabel="Open reservations"
      busy={busy}
      busyLabel="Opening reservations…"
      onConfirm={open}
      onClose={() => {
        setError(null)
        onClose()
      }}
    >
      {date && (
        <div className="flex flex-col gap-3">
          <p>These terms lock now and cannot change afterwards.</p>
          <TermsList
            items={lockedTerms({
              timeZone: w.timezone,
              dateLabel: formatDateRange(date.starts_at, date.ends_at, w.timezone),
              price: w.price,
              windowMin: w.payment_window_min,
              minimum: w.minimum,
              capacity: w.capacity,
              closesAbout: new Date(Date.now() + w.payment_window_min * 60000).toISOString(),
            })}
          />
          <p className="text-sm">
            Each person reserves with a PayPal hold. Holds are charged once enough people have reserved; if not enough people pay by the deadline, the
            workshop is cancelled, holds are released and any charge is refunded.
          </p>
          {error && (
            <Notice tone="attention" role="alert">
              {error}
            </Notice>
          )}
        </div>
      )}
    </ConfirmDialog>
  )
}
