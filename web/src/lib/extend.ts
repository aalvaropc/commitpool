import type { Extension, Proposal, WorkshopStatus } from '../api'

export const DEMO_LAST_HOURS_MS = 2 * 60 * 60 * 1000

export type ExtendLink = 'open' | 'stale' | null

export function extendLink(param: string | null, extension: Extension | null | undefined): ExtendLink {
  if (param === null) return null
  const at = Number(param)
  if (!Number.isInteger(at) || !extension?.available || !extension.to) return 'stale'
  return at * 1000 === Date.parse(extension.to) ? 'open' : 'stale'
}

export function movedDeadline(proposal: Proposal | null | undefined, status: WorkshopStatus): { from: string; to: string } | null {
  if (!proposal?.extended_from || status !== 'FUNDING' || Date.parse(proposal.closes_at) <= Date.parse(proposal.extended_from)) return null
  return { from: proposal.extended_from, to: proposal.closes_at }
}

export function latestDeadline(proposal: Proposal, status: WorkshopStatus): string | null {
  return status === 'FUNDING' && proposal.payments_open && proposal.latest_closes_at ? proposal.latest_closes_at : null
}

export function canSkipToLastHours(proposal: Proposal, now: number): boolean {
  return !proposal.extended_from && Date.parse(proposal.closes_at) - now > DEMO_LAST_HOURS_MS
}
