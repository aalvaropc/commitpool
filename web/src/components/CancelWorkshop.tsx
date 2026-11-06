import { useState } from 'react'
import { api, type OwnerView } from '../api'
import { canCancel, cancelEffect } from '../lib/actions'
import { Button, Field, Notice, describedBy, inputClass } from './ui'

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
      setError(e instanceof Error ? e.message : 'The workshop could not be cancelled right now.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <section aria-labelledby="cancel-title" className="flex flex-col gap-3">
      <h2 id="cancel-title" className="text-xl font-bold">
        Cancel the workshop
      </h2>
      {!asking ? (
        <>
          <p className="text-sm text-ink-soft">If it cannot go ahead, cancel it: every hold is released and every payment refunded.</p>
          <div>
            <Button variant="secondary" onClick={() => setAsking(true)}>
              Cancel workshop…
            </Button>
          </div>
        </>
      ) : (
        <div className="flex flex-col gap-4 rounded-sm border border-rule border-t-4 border-t-cancel bg-sheet p-4">
          <p id="cancel-effect" className="font-semibold">
            {cancelEffect(view.enrollments)}
          </p>
          <p className="text-sm text-ink-soft">Participants who left an email are told. This cannot be undone.</p>
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
          <div className="flex flex-wrap gap-3">
            <Button variant="danger" onClick={cancel} busy={busy} busyLabel="Cancelling…" aria-describedby="cancel-effect">
              Yes, cancel workshop
            </Button>
            <Button variant="secondary" onClick={() => setAsking(false)} disabled={busy}>
              Keep it
            </Button>
          </div>
        </div>
      )}
      {error && (
        <Notice tone="attention" role="alert">
          {error}
        </Notice>
      )}
    </section>
  )
}
