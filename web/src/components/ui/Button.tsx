import type { ButtonHTMLAttributes, MouseEvent, Ref } from 'react'
import { Link, type LinkProps } from 'react-router'

export type ButtonVariant = 'primary' | 'secondary' | 'quiet' | 'danger' | 'demo' | 'inverse' | 'ink' | 'outline'
export type ButtonSize = 'sm' | 'md' | 'lg'

const variants: Record<ButtonVariant, string> = {
  primary: 'bg-brand text-on-brand hover:bg-brand-deep',
  secondary: 'bg-surface-muted text-ink hover:bg-line',
  quiet: 'bg-transparent text-brand hover:bg-surface',
  danger: 'bg-no text-canvas hover:brightness-110',
  demo: 'bg-surface text-ink hover:bg-surface-muted',
  inverse: 'bg-ink text-canvas hover:bg-white',
  ink: 'bg-on-brand text-ink hover:bg-brand-wash',
  outline: 'border-2 border-on-brand bg-transparent text-on-brand hover:bg-on-brand/10',
}

const sizes: Record<ButtonSize, string> = {
  sm: 'min-h-11 rounded-full px-4 text-sm font-semibold',
  md: 'min-h-11 rounded-full px-5 text-base font-semibold',
  lg: 'min-h-14 rounded-full px-7 text-lg font-bold',
}

const base =
  'inline-flex items-center justify-center gap-2 leading-tight transition-[background-color,transform] duration-200 ease-(--ease-out) active:translate-y-px disabled:cursor-not-allowed disabled:opacity-55 disabled:active:translate-y-0 aria-disabled:cursor-progress aria-disabled:opacity-70 aria-disabled:active:translate-y-0'

export function buttonClass(variant: ButtonVariant = 'primary', size: ButtonSize = 'md', block = false): string {
  return `${base} ${variants[variant]} ${sizes[size]} ${block ? 'w-full' : ''}`
}

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  ref?: Ref<HTMLButtonElement>
  variant?: ButtonVariant
  size?: ButtonSize
  block?: boolean
  busy?: boolean
  busyLabel?: string
}

export function Button({ variant = 'primary', size = 'md', block = false, busy = false, busyLabel, className = '', children, type = 'button', disabled, onClick, ...rest }: ButtonProps) {
  function click(e: MouseEvent<HTMLButtonElement>) {
    if (busy) {
      e.preventDefault()
      return
    }
    onClick?.(e)
  }
  return (
    <button
      type={type}
      disabled={disabled || (busy && type === 'submit')}
      aria-disabled={busy || undefined}
      aria-busy={busy || undefined}
      className={`${buttonClass(variant, size, block)} ${className}`}
      onClick={click}
      {...rest}
    >
      {busy && busyLabel ? busyLabel : children}
    </button>
  )
}

interface ButtonLinkProps extends LinkProps {
  variant?: ButtonVariant
  size?: ButtonSize
  block?: boolean
}

export function ButtonLink({ variant = 'primary', size = 'md', block = false, className = '', ...rest }: ButtonLinkProps) {
  return <Link className={`${buttonClass(variant, size, block)} no-underline ${className}`} {...rest} />
}
