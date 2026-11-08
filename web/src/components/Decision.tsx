import type { OwnerView } from '../api'
import type { NextStep as Step } from '../lib/owner'
import CopyField from './CopyField'
import { Button, Icon, Tile } from './ui'

interface Props {
  view: OwnerView
  step: Step
  onOpen: (dateId: string) => void
}

export function InviteLink({ url, hint }: { url?: string; hint: string }) {
  if (!url) return <p className="text-sm text-ink-soft">This workshop was created before invite links were kept, so there is none to show.</p>
  return <CopyField id="share-url" label="Invite link" value={url} hint={hint} />
}

export default function NextStep({ view, step, onOpen }: Props) {
  const action = step.action
  return (
    <Tile as="section" aria-labelledby="next-title" className="flex flex-col gap-4 sm:p-7">
      <h2 id="next-title" className="sr-only">
        Next step
      </h2>
      <p className="max-w-[30ch] font-display text-2xl leading-tight font-bold sm:text-[2rem]">{step.title}</p>
      <p className="max-w-prose text-ink-soft">{step.detail}</p>
      {action.kind === 'open' && (
        <div>
          <Button size="lg" onClick={() => onOpen(action.dateId)}>
            Open reservations for {action.day}
            <Icon name="arrowRight" size={20} />
          </Button>
        </div>
      )}
      {action.kind === 'share' && <InviteLink url={view.share_url} hint="Send it to your group chat. Anyone with it can join and say when they can come." />}
    </Tile>
  )
}
