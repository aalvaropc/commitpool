import { useEffect, useId, useRef, type ReactNode } from 'react'
import { Button, type ButtonVariant } from './Button'

interface ConfirmDialogProps {
  open: boolean
  title: string
  children: ReactNode
  confirmLabel: string
  cancelLabel?: string
  busyLabel?: string
  busy?: boolean
  tone?: Extract<ButtonVariant, 'primary' | 'danger' | 'go'>
  onConfirm: () => void
  onClose: () => void
}

export function ConfirmDialog({ open, title, children, confirmLabel, cancelLabel = 'Not now', busyLabel, busy = false, tone = 'primary', onConfirm, onClose }: ConfirmDialogProps) {
  const ref = useRef<HTMLDialogElement>(null)
  const titleId = useId()
  const bodyId = useId()

  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    if (open && !dialog.open) dialog.showModal()
    if (!open && dialog.open) dialog.close()
  }, [open])

  function closed() {
    if (!open) return
    if (busy) {
      ref.current?.showModal()
      return
    }
    onClose()
  }

  return (
    <dialog
      ref={ref}
      aria-labelledby={titleId}
      aria-describedby={bodyId}
      onCancel={(e) => {
        e.preventDefault()
        if (!busy) onClose()
      }}
      onClose={closed}
      className="m-auto w-[min(32rem,calc(100vw-2rem))] rounded-tile bg-surface p-0 text-ink shadow-lift backdrop:bg-ink/40 backdrop:backdrop-blur-[2px]"
    >
      <div className="flex flex-col gap-4 p-5 sm:p-6">
        <h2 id={titleId} className="text-xl">
          {title}
        </h2>
        <div id={bodyId} className="text-ink-soft">
          {children}
        </div>
        <p role="status" className="sr-only">
          {busy ? busyLabel ?? 'Working…' : ''}
        </p>
        <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <Button variant="secondary" onClick={onClose} busy={busy}>
            {cancelLabel}
          </Button>
          <Button variant={tone} onClick={onConfirm} busy={busy} busyLabel={busyLabel}>
            {confirmLabel}
          </Button>
        </div>
      </div>
    </dialog>
  )
}
