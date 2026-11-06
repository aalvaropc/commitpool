import type { Tone } from '../../lib/status'

export const toneText: Record<Tone, string> = {
  neutral: 'text-ink',
  hold: 'text-brand',
  paid: 'text-paid',
  cancel: 'text-no',
  attention: 'text-attention',
}

export const toneWash: Record<Tone, string> = {
  neutral: 'bg-surface-muted',
  hold: 'bg-brand-wash',
  paid: 'bg-paid-wash',
  cancel: 'bg-no-wash',
  attention: 'bg-attention-wash',
}

export const toneFill: Record<Tone, string> = {
  neutral: 'bg-line-strong',
  hold: 'bg-brand',
  paid: 'bg-paid',
  cancel: 'bg-no',
  attention: 'bg-attention',
}
