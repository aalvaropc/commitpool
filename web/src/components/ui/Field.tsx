import type { ReactNode } from 'react'

export const inputClass =
  'w-full min-h-11 rounded-control border border-line-strong bg-surface px-3.5 py-2.5 text-base text-ink transition-[border-color,box-shadow] duration-150 hover:border-ink-soft focus:border-brand focus:shadow-[0_0_0_3px_var(--color-brand-wash)] focus:outline-none aria-[invalid=true]:border-no disabled:bg-surface-muted disabled:opacity-70'

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
