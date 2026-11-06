import type { HTMLAttributes, ReactNode } from 'react'

type TileTone = 'surface' | 'brand' | 'muted'
type TileElement = 'div' | 'section' | 'article' | 'aside' | 'header' | 'li'

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

interface TileLabelProps {
  as?: 'p' | 'h2' | 'h3'
  id?: string
  children: ReactNode
  className?: string
}

export function TileLabel({ as: Element = 'p', id, children, className = '' }: TileLabelProps) {
  return (
    <Element id={id} className={`font-sans text-xs leading-normal font-semibold tracking-[0.06em] text-ink-soft uppercase ${className}`}>
      {children}
    </Element>
  )
}
