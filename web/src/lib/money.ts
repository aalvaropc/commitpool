export interface MinorAmount {
  minor: number
  currency: string
}

export function formatMoney({ minor, currency }: MinorAmount): string {
  const format = new Intl.NumberFormat('en-US', { style: 'currency', currency })
  const digits = format.resolvedOptions().maximumFractionDigits ?? 2
  return format.format(minor / 10 ** digits)
}
