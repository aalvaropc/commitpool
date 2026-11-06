import { count } from './plural'

export interface MeterInput {
  capacity: number
  needed: number
  paid: number
  reserved: number
}

export interface MeterModel {
  paidPct: number
  reservedPct: number
  neededPct: number
  reached: boolean
  caption: string
  summary: string
}

function percent(part: number, whole: number): number {
  return whole > 0 ? (part / whole) * 100 : 0
}

function caption({ capacity, needed, paid, reserved }: MeterInput): string {
  if (paid >= needed) return paid >= capacity ? 'It’s on — full' : `It’s on — ${paid} paid`
  if (paid + reserved >= needed) return 'Enough reserved — confirming payments'
  return `${needed - paid - reserved} more and it’s on`
}

export function meter(input: MeterInput): MeterModel {
  const capacity = Math.max(input.capacity, 0)
  const paid = Math.min(Math.max(input.paid, 0), capacity)
  const reserved = Math.min(Math.max(input.reserved, 0), capacity - paid)
  return {
    paidPct: percent(paid, capacity),
    reservedPct: percent(reserved, capacity),
    neededPct: percent(Math.min(input.needed, capacity), capacity),
    reached: input.paid >= input.needed,
    caption: caption(input),
    summary: `${input.paid} paid and ${input.reserved} reserved of ${count(input.capacity, 'spot')}; ${input.needed} needed to happen.`,
  }
}
