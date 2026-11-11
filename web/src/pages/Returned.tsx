import { useSearchParams } from 'react-router'
import { api } from '../api'
import { ButtonLink, Icon, Page, Tile, toneText, toneWash } from '../components/ui'
import { useResource } from '../lib/hooks'
import { storedParticipant } from '../lib/participant'
import { returnedMessage, returnPath } from '../lib/returned'

export default function Returned() {
  const [params] = useSearchParams()
  const result = params.get('result')
  const token = result === 'authorized' ? storedParticipant() : null
  const view = useResource(() => (token ? api.participant(token) : Promise.resolve(null)), token ?? '')
  const message = returnedMessage(result, view.data)
  const reading = token !== null && !view.data && !view.error
  const back = returnPath(result)

  return (
    <Page>
      <title>{`${message.title} – CommitPool`}</title>
      <Tile className="flex flex-col items-start gap-5 sm:p-8">
        <h1 className="text-2xl text-ink-soft">Back from PayPal</h1>
        <div role={message.tone === 'attention' ? 'alert' : 'status'} className="flex flex-col gap-3">
          <span aria-hidden="true" className={`grid size-12 place-items-center rounded-full ${toneWash[message.tone]} ${toneText[message.tone]}`}>
            <Icon name={result === 'pending' ? 'clock' : message.tone === 'hold' || message.tone === 'paid' ? 'check' : message.tone === 'neutral' ? 'help' : 'x'} size={24} />
          </span>
          <p className="font-display text-3xl font-bold">{message.title}</p>
          {!reading && <p className="max-w-prose text-ink-soft">{message.body}</p>}
        </div>
        {back ? (
          <ButtonLink to={back} size="lg">
            Go to your workshop page
          </ButtonLink>
        ) : (
          <p className="max-w-prose text-ink-soft">Open the personal link you got when you joined to see your status. This browser tab does not have it.</p>
        )}
      </Tile>
    </Page>
  )
}
