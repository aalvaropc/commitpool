export interface MinorAmount {
  minor: number
  currency: string
}

/** Formats an amount kept in minor units, e.g. {minor: 1000, currency: "USD"} → "$10.00". */
export function formatMoney({ minor, currency }: MinorAmount): string {
  const format = new Intl.NumberFormat('en-US', { style: 'currency', currency })
  const digits = format.resolvedOptions().maximumFractionDigits ?? 2
  return format.format(minor / 10 ** digits)
}
