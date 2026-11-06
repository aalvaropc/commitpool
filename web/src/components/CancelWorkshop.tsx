import { useState } from 'react'
import { api, type OwnerView } from '../api'
import { canCancel, cancelEffect } from '../lib/actions'
import { Button, ConfirmDialog, Field, Notice, describedBy, inputClass } from './ui'

interface Props {
  token: string
  view: OwnerView
  onChange: () => Promise<unknown>
}

const MAX_REASON = 280

export default function CancelWorkshop({ token, view, onChange }: Props) {
  const [asking, setAsking] = useState(false)
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  if (!canCancel(view.workshop.status, view.proposal?.starts_at, Date.now())) return null

  async function cancel() {
    setBusy(true)
    setError(null)
    try {
      await api.cancelWorkshop(token, reason.trim())
      setAsking(false)
      await onChange()
    } catch (e) {
      setAsking(false)
      setError(e instanceof Error ? e.message : 'The workshop could not be cancelled right now.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <section aria-labelledby="cancel-title" className="flex flex-col gap-2">
      <h3 id="cancel-title" className="text-base">
        Cancel the workshop
      </h3>
      <p className="text-sm text-ink-soft">If it cannot go ahead: every hold is released and every payment refunded.</p>
      <div>
        <Button variant="secondary" size="sm" onClick={() => setAsking(true)}>
          Cancel workshop…
        </Button>
      </div>
      {error && (
        <Notice tone="attention" role="alert">
          {error}
        </Notice>
      )}
      <ConfirmDialog
        open={asking}
        title="Cancel this workshop?"
        confirmLabel="Yes, cancel workshop"
        cancelLabel="Keep it"
        busy={busy}
        busyLabel="Cancelling…"
        tone="danger"
        onConfirm={cancel}
        onClose={() => setAsking(false)}
      >
        <div className="flex flex-col gap-3">
          <p className="font-semibold text-ink">{cancelEffect(view.enrollments)}</p>
          <p className="text-sm">Participants who left an email are told. This cannot be undone.</p>
          <Field id="cancel-reason" label="Reason (optional)" hint="Included in the email participants get.">
            <textarea
              id="cancel-reason"
              rows={2}
              maxLength={MAX_REASON}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              aria-describedby={describedBy('cancel-reason', true)}
              className={inputClass}
            />
          </Field>
        </div>
      </ConfirmDialog>
    </section>
  )
}
