import type { ButtonHTMLAttributes, ReactNode, Ref } from 'react'
import { Link, type LinkProps } from 'react-router'
import type { Tone } from '../lib/status'

export const toneText: Record<Tone, string> = {
  neutral: 'text-ink',
  hold: 'text-hold',
  paid: 'text-paid',
  cancel: 'text-cancel',
  attention: 'text-attention',
}

export const toneWash: Record<Tone, string> = {
  neutral: 'bg-sheet',
  hold: 'bg-hold-wash',
  paid: 'bg-paid-wash',
  cancel: 'bg-cancel-wash',
  attention: 'bg-attention-wash',
}

export const toneFill: Record<Tone, string> = {
  neutral: 'bg-edge',
  hold: 'bg-hold-fill',
  paid: 'bg-paid-fill',
  cancel: 'bg-cancel',
  attention: 'bg-attention',
}

type Variant = 'primary' | 'secondary' | 'danger' | 'demo'

const variants: Record<Variant, string> = {
  primary: 'bg-signal text-white hover:bg-signal-deep',
  secondary: 'border border-edge bg-sheet text-ink hover:bg-paper',
  danger: 'bg-cancel text-white hover:opacity-90',
  demo: 'border border-dashed border-edge bg-transparent text-ink hover:bg-sheet',
}

const buttonBase =
  'inline-flex items-center justify-center gap-2 rounded-md px-4 py-2.5 font-semibold leading-tight transition-colors disabled:cursor-not-allowed disabled:opacity-60'

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  ref?: Ref<HTMLButtonElement>
  variant?: Variant
  busy?: boolean
  busyLabel?: string
}

export function Button({ variant = 'primary', busy = false, busyLabel, className = '', children, disabled, type, ...rest }: ButtonProps) {
  return (
    <button
      type={type ?? 'button'}
      disabled={disabled || busy}
      aria-busy={busy || undefined}
      className={`${buttonBase} ${variants[variant]} ${className}`}
      {...rest}
    >
      {busy && busyLabel ? busyLabel : children}
    </button>
  )
}

export function ButtonLink({ variant = 'primary', className = '', ...rest }: LinkProps & { variant?: Variant }) {
  return <Link className={`${buttonBase} ${variants[variant]} no-underline ${className}`} {...rest} />
}

export function DemoTag() {
  return <span className="rounded-sm bg-hold-wash px-1.5 py-0.5 text-xs font-semibold text-hold">Demo only</span>
}

export const inputClass =
  'w-full rounded-md border border-edge bg-sheet px-3 py-2.5 text-base text-ink placeholder:text-ink-soft aria-[invalid=true]:border-cancel disabled:opacity-70'

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
        <p id={`${id}-error`} className="text-sm font-medium text-cancel">
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

interface NoticeProps {
  tone?: Tone
  title?: string
  role?: 'alert' | 'status'
  children: ReactNode
  className?: string
}

export function Notice({ tone = 'neutral', title, role = 'status', children, className = '' }: NoticeProps) {
  return (
    <div role={role} className={`rounded-sm border border-rule px-4 py-3 ${toneWash[tone]} ${className}`}>
      {title && <p className={`font-semibold ${toneText[tone]}`}>{title}</p>}
      <div className={title ? 'mt-0.5 text-ink' : toneText[tone] === 'text-ink' ? 'text-ink' : `font-medium ${toneText[tone]}`}>
        {children}
      </div>
    </div>
  )
}

export function Tag({ tone, children }: { tone: Tone; children: ReactNode }) {
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-sm px-2 py-0.5 text-sm font-semibold ${toneWash[tone]} ${toneText[tone]}`}>
      <span aria-hidden="true" className={`size-2 ${toneFill[tone]}`} />
      {children}
    </span>
  )
}

export function Page({ children, wide = false }: { children: ReactNode; wide?: boolean }) {
  return <div className={`mx-auto w-full ${wide ? 'max-w-6xl' : 'max-w-3xl'} px-4 py-8 sm:px-6 sm:py-12`}>{children}</div>
}
