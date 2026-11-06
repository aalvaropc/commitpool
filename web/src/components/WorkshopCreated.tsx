import { useEffect, useRef } from 'react'
import type { CreateWorkshopResponse } from '../api'
import { inviteMessage } from '../lib/workshopForm'
import CopyField from './CopyField'
import { ButtonLink, Icon, Notice, Tile } from './ui'

export default function WorkshopCreated({ created }: { created: CreateWorkshopResponse }) {
  const heading = useRef<HTMLHeadingElement>(null)
  useEffect(() => heading.current?.focus(), [])
  const ownerPath = new URL(created.owner_url).pathname
  return (
    <div className="flex flex-col gap-6">
      <title>Workshop created – CommitPool</title>
      <header className="flex flex-col gap-2">
        <p className="flex items-center gap-1.5 text-sm font-semibold text-paid">
          <Icon name="check" size={16} />
          Created
        </p>
        <h1 ref={heading} tabIndex={-1} className="text-3xl sm:text-5xl">
          {created.workshop.title} is ready
        </h1>
      </header>
      <Notice tone="attention" title="Save your organizer link now" role="alert">
        It is the only way back to your organizer page and it will not be shown again. Anyone who has it can open reservations.
      </Notice>
      <Tile className="flex flex-col gap-2 sm:p-6">
        <CopyField id="owner-url" label="Organizer link (keep it private)" value={created.owner_url} />
      </Tile>
      <Tile className="flex flex-col gap-4 sm:p-6">
        <CopyField id="invite-message" label="Message for your group chat" value={inviteMessage(created)} hint="Paste it where your group already talks. It carries the invite link." multiline />
        <CopyField id="share-url" label="Invite link" value={created.share_url} hint="The same link on its own, for anywhere else." />
      </Tile>
      <div>
        <ButtonLink to={ownerPath} size="lg">
          Open the organizer page
          <Icon name="arrowRight" size={20} />
        </ButtonLink>
      </div>
    </div>
  )
}
