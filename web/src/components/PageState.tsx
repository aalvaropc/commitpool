import type { ApiError } from '../api'
import { Button, ButtonLink, Notice, Page } from './ui'

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

/** A refresh of a page already shown failed: it says so, and offers a refresh unless polling will retry. */
export function RefreshError({ polling, onRetry }: { polling: boolean; onRetry: () => void }) {
  return (
    <Notice tone="attention">
      {polling ? (
        'This page could not refresh. It will try again in a few seconds.'
      ) : (
        <span className="flex flex-wrap items-center gap-3">
          This page could not refresh.
          <Button variant="secondary" onClick={onRetry} className="px-3 py-1.5 text-sm">
            Refresh
          </Button>
        </span>
      )}
    </Notice>
  )
}
