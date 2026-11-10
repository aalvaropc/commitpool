import type { Proposal, WorkshopStatus } from '../api'
import { movedDeadline } from '../lib/extend'
import { formatMoment } from '../lib/time'
import { Notice } from './ui'

interface Props {
  proposal: Proposal | null
  status: WorkshopStatus
  timeZone: string
  audience: 'owner' | 'participant'
}

export default function DeadlineMoved({ proposal, status, timeZone, audience }: Props) {
  const moved = movedDeadline(proposal, status)
  if (!moved) return null
  return (
    <Notice tone="hold" title={`Deadline extended from ${formatMoment(moved.from, timeZone)} to ${formatMoment(moved.to, timeZone)}`}>
      {audience === 'owner'
        ? 'You moved it once, and it cannot move again. Everyone who left an email was told; holds stay in place.'
        : 'The organizer moved it once to give more people time, and it cannot move again. Your spot stays as it is, and you can still leave until charging starts.'}
    </Notice>
  )
}
