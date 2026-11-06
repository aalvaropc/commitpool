import type { ButtonHTMLAttributes, MouseEvent, Ref } from 'react'
import { Link, type LinkProps } from 'react-router'

export type ButtonVariant = 'primary' | 'go' | 'secondary' | 'quiet' | 'danger' | 'demo'
export type ButtonSize = 'sm' | 'md' | 'lg'

const variants: Record<ButtonVariant, string> = {
  primary: 'bg-brand text-white shadow-tile hover:bg-brand-deep',
  go: 'bg-go text-white shadow-go hover:bg-go-deep',
  secondary: 'border border-line-strong bg-surface text-ink shadow-tile hover:border-ink-soft',
  quiet: 'bg-transparent text-brand hover:bg-brand-wash',
  danger: 'bg-no text-white shadow-tile hover:brightness-110',
  demo: 'border border-dashed border-line-strong bg-transparent text-ink hover:bg-surface',
}

const smallGo = 'bg-go-deep text-white shadow-go hover:bg-[#9a3412]'

const sizes: Record<ButtonSize, string> = {
  sm: 'min-h-11 rounded-[0.75rem] px-3 text-sm font-semibold',
  md: 'min-h-11 rounded-control px-4 text-base font-semibold',
  lg: 'min-h-14 rounded-[1.125rem] px-6 text-[1.1875rem] font-bold',
}

const base =
  'inline-flex items-center justify-center gap-2 leading-tight transition-[background-color,border-color,box-shadow,transform] duration-200 ease-(--ease-out) active:translate-y-px disabled:cursor-not-allowed disabled:opacity-55 disabled:active:translate-y-0 aria-disabled:cursor-progress aria-disabled:opacity-70 aria-disabled:active:translate-y-0'

export function buttonClass(variant: ButtonVariant = 'primary', size: ButtonSize = 'md', block = false): string {
  const look = variant === 'go' && size !== 'lg' ? smallGo : variants[variant]
  return `${base} ${look} ${sizes[size]} ${block ? 'w-full' : ''}`
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
