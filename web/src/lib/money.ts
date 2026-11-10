export interface MinorAmount {
  minor: number
  currency: string
}

export function formatMoney({ minor, currency }: MinorAmount): string {
  const digits = new Intl.NumberFormat('en-US', { style: 'currency', currency }).resolvedOptions().maximumFractionDigits ?? 2
  const whole = minor % 10 ** digits === 0
  return new Intl.NumberFormat('en-US', { style: 'currency', currency, minimumFractionDigits: whole ? 0 : digits }).format(minor / 10 ** digits)
}

export function formatAmount({ minor, currency }: MinorAmount): string {
  const digits = new Intl.NumberFormat('en-US', { style: 'currency', currency }).resolvedOptions().maximumFractionDigits ?? 2
  return new Intl.NumberFormat('en-US', { style: 'currency', currency, minimumFractionDigits: digits }).format(minor / 10 ** digits)
}
