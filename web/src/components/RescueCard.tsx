import { useState } from 'react'
import { api, ApiError, type Extension, type OwnerView, type Suggestion } from '../api'
import { formatMoment } from '../lib/time'
import { Button, ConfirmDialog, Icon, Notice } from './ui'

export interface Rescue {
  extension: Extension | null
  timeZone: string
  onExtend: () => void
}

export function RescueCard({ s, rescue }: { s: Suggestion; rescue?: Rescue }) {
  const [copied, setCopied] = useState<boolean | null>(null)
  const to = rescue?.extension?.available ? rescue.extension.to : undefined

  async function copy() {
    try {
      await navigator.clipboard.writeText(s.message)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }

  return (
    <li className="flex flex-col gap-3 rounded-control bg-brand-wash p-4 ring-1 ring-brand/40 ring-inset">
      <p className="flex min-w-0 items-start gap-2.5 font-semibold">
        <Icon name="clock" className="mt-0.5 shrink-0 text-brand" />
        <span className="min-w-0 [overflow-wrap:anywhere]">{s.reason}</span>
      </p>
      {to && rescue && (
        <div>
          <Button onClick={rescue.onExtend}>Extend to {formatMoment(to, rescue.timeZone)}</Button>
        </div>
      )}
      <div className="flex flex-col gap-1.5">
        <p className="text-sm font-semibold text-ink-soft">Claude’s message for the group, once you extend</p>
        <p className="rounded-control bg-surface px-4 py-3 whitespace-pre-line [overflow-wrap:anywhere]">{s.message}</p>
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <Button variant="secondary" size="sm" onClick={() => void copy()}>
          <Icon name="copy" size={16} />
          Copy message<span className="sr-only"> for the group</span>
        </Button>
        <span role="status" className="text-sm text-ink-soft">
          {copied ? 'Copied. Send it to the group once you extend.' : copied === false ? 'Copying is blocked here; select the message and copy it.' : s.pending ? 'Claude is rewording this message…' : ''}
        </span>
      </div>
    </li>
  )
}

interface DialogProps {
  token: string
  view: OwnerView
  open: boolean
  onClose: () => void
  onExtended: () => Promise<unknown>
}

export function ExtendDialog({ token, view, open, onClose, onExtended }: DialogProps) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const p = view.proposal
  const x = view.extension
  const to = x?.available ? x.to : undefined
  const tz = view.workshop.timezone

  async function extend() {
    if (!p || !to) return
    setBusy(true)
    setError(null)
    try {
      await api.extendDeadline(token, p.closes_at, to)
      onClose()
      await onExtended()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'The deadline could not be extended.')
      if (e instanceof ApiError) await onExtended()
    } finally {
      setBusy(false)
    }
  }

  const when = to ? formatMoment(to, tz) : ''
  return (
    <ConfirmDialog
      open={open && p !== null && (to !== undefined || error !== null)}
      title={to ? `Extend the deadline to ${when}?` : 'The deadline cannot be extended'}
      confirmLabel={to ? `Extend to ${when}` : 'Close'}
      busy={busy}
      busyLabel="Extending the deadline…"
      onConfirm={to ? () => void extend() : onClose}
      onClose={() => {
        setError(null)
        onClose()
      }}
    >
      {p && (
        <div className="flex flex-col gap-3">
          {to ? (
            <>
              <p>
                Reservations close {formatMoment(p.closes_at, tz)}. Extending moves the deadline to {when}.
              </p>
              <ul className="flex list-disc flex-col gap-1 pl-5 text-ink">
                <li>You can do this only once.</li>
                <li>Everyone who left an email is told the new deadline.</li>
                <li>Holds stay in place: nobody reserves or pays again.</li>
                <li>Anyone can still leave and release their hold until charging starts.</li>
              </ul>
            </>
          ) : (
            <p>{x?.reason}</p>
          )}
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
