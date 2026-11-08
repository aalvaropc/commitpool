import type { OwnerView } from '../api'
import { tokenFromParticipantURL } from './participant'

export interface DemoParticipant {
  name: string
  path: string
}

export function demoParticipants(view: Pick<OwnerView, 'act_as'>): DemoParticipant[] {
  return (view.act_as ?? []).flatMap((p) => {
    const token = tokenFromParticipantURL(p.participant_url)
    return token ? [{ name: p.name, path: `/p/${token}` }] : []
  })
}
