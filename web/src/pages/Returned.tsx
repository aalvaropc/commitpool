import { useSearchParams } from 'react-router'
import { ButtonLink, Notice, Page } from '../components/ui'
import { returnedMessage, returnPath } from '../lib/returned'

export default function Returned() {
  const [params] = useSearchParams()
  const result = params.get('result')
  const message = returnedMessage(result)
  const back = returnPath(result)

  return (
    <Page>
      <title>{`${message.title} – CommitPool`}</title>
      <div className="flex flex-col gap-6">
        <h1 className="text-3xl font-bold">Back from PayPal</h1>
        <Notice tone={message.tone} title={message.title} role={message.tone === 'attention' ? 'alert' : 'status'}>
          <p className="max-w-prose">{message.body}</p>
        </Notice>
        {back ? (
          <div>
            <ButtonLink to={back}>Go to your workshop page</ButtonLink>
          </div>
        ) : (
          <p className="max-w-prose text-ink-soft">
            Open the personal link you got when you joined the workshop to see your status. This browser tab does not have it.
          </p>
        )}
      </div>
    </Page>
  )
}
