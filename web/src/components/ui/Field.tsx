import type { ReactNode } from 'react'

export const inputClass =
  'w-full min-h-12 rounded-control border border-line-strong bg-surface-muted px-4 py-3 text-base text-ink transition-colors duration-150 hover:bg-line focus-visible:bg-line aria-[invalid=true]:border-no aria-[invalid=true]:bg-no-wash disabled:opacity-60'

interface FieldProps {
  id: string
  label: string
  hint?: ReactNode
  error?: string
  children: ReactNode
}

export function Field({ id, label, hint, error, children }: FieldProps) {
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={id} className="font-semibold">
        {label}
      </label>
      {children}
      {hint && (
        <p id={`${id}-hint`} className="text-sm text-ink-soft">
          {hint}
        </p>
      )}
      {error && (
        <p id={`${id}-error`} className="text-sm font-medium text-no">
          {error}
        </p>
      )}
    </div>
  )
}

export function describedBy(id: string, hint: boolean, error?: string): string | undefined {
  const ids = [hint ? `${id}-hint` : '', error ? `${id}-error` : ''].filter(Boolean)
  return ids.length ? ids.join(' ') : undefined
}
