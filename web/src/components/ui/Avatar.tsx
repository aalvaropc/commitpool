import { avatarTone, initials, type AvatarTone } from '../../lib/avatar'

const tones: Record<AvatarTone, string> = {
  amber: 'bg-avatar-amber text-avatar-amber-ink',
  sky: 'bg-avatar-sky text-avatar-sky-ink',
  pink: 'bg-avatar-pink text-avatar-pink-ink',
  mint: 'bg-avatar-mint text-avatar-mint-ink',
  lilac: 'bg-avatar-lilac text-avatar-lilac-ink',
  peach: 'bg-avatar-peach text-avatar-peach-ink',
}

const sizes = { sm: 'size-8 text-xs', md: 'size-10 text-sm', lg: 'size-12 text-base' } as const

interface AvatarProps {
  name: string
  size?: keyof typeof sizes
  ring?: boolean
  className?: string
}

export function Avatar({ name, size = 'md', ring = false, className = '' }: AvatarProps) {
  return (
    <span
      aria-hidden="true"
      className={`inline-grid shrink-0 place-items-center rounded-full font-display font-bold ${sizes[size]} ${tones[avatarTone(name)]} ${ring ? 'ring-3 ring-surface' : ''} ${className}`}
    >
      {initials(name)}
    </span>
  )
}

export function AvatarStack({ names, max = 5, label }: { names: string[]; max?: number; label: string }) {
  const shown = names.slice(0, max)
  const more = names.length - shown.length
  return (
    <div className="flex items-center" role="img" aria-label={label}>
      {shown.map((n, i) => (
        <Avatar key={`${n}-${i}`} name={n} ring className={i > 0 ? '-ml-2.5' : ''} />
      ))}
      {more > 0 && (
        <span aria-hidden="true" className="-ml-2.5 inline-grid size-10 place-items-center rounded-full bg-surface-muted font-display text-sm font-bold text-ink-soft ring-3 ring-surface">
          +{more}
        </span>
      )}
    </div>
  )
}
