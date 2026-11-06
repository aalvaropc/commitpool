import type { HTMLAttributes, ReactNode } from 'react'

type TileTone = 'surface' | 'brand' | 'muted'
type TileElement = 'div' | 'section' | 'article' | 'aside' | 'li'

const tones: Record<TileTone, string> = {
  surface: 'bg-surface text-ink',
  brand: 'bg-brand text-white [&_:focus-visible]:outline-white',
  muted: 'bg-surface-muted text-ink',
}

interface TileProps extends HTMLAttributes<HTMLElement> {
  as?: TileElement
  tone?: TileTone
  padded?: boolean
  children: ReactNode
}

export function Tile({ as: Element = 'div', tone = 'surface', padded = true, className = '', children, ...rest }: TileProps) {
  return (
    <Element className={`rounded-tile shadow-tile ${tones[tone]} ${padded ? 'p-4 sm:p-5' : ''} ${className}`} {...rest}>
      {children}
    </Element>
  )
}

export function TileLabel({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <p className={`text-xs font-semibold tracking-[0.06em] text-ink-soft uppercase ${className}`}>{children}</p>
}
