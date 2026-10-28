import type { ApiError } from '../api'
import { Button, ButtonLink, Page } from './ui'

export function Loading({ what }: { what: string }) {
  return (
    <Page>
      <p role="status" className="text-ink-soft">
        Loading {what}…
      </p>
    </Page>
  )
}

interface LoadErrorProps {
  error: ApiError
  /** What a 404 means on this page, in one sentence. */
  missing: string
  onRetry: () => void
}

export function LoadError({ error, missing, onRetry }: LoadErrorProps) {
  const notFound = error.status === 404
  return (
    <Page>
      <div role="alert" className="flex flex-col gap-3">
        <h1 className="text-2xl font-bold">{notFound ? 'This link does not open anything' : 'This page could not load'}</h1>
        <p className="max-w-prose text-ink-soft">{notFound ? missing : error.message}</p>
        <div className="flex flex-wrap gap-3 pt-2">
          {!notFound && <Button onClick={onRetry}>Try again</Button>}
          <ButtonLink to="/" variant="secondary">
            Go to the start page
          </ButtonLink>
        </div>
      </div>
    </Page>
  )
}
