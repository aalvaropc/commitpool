import type { ApiError } from '../api'
import { Button, ButtonLink, Notice, Page, Tile } from './ui'

export function Loading({ what }: { what: string }) {
  return (
    <Page>
      <div role="status" className="flex flex-col gap-3">
        <span className="sr-only">Loading {what}…</span>
        <div aria-hidden="true" className="h-52 animate-pulse rounded-tile bg-brand-wash" />
        <div aria-hidden="true" className="grid grid-cols-2 gap-3">
          <div className="h-28 animate-pulse rounded-tile bg-surface" />
          <div className="h-28 animate-pulse rounded-tile bg-surface" />
        </div>
      </div>
    </Page>
  )
}

interface LoadErrorProps {
  error: ApiError
  missing: string
  onRetry: () => void
}

export function LoadError({ error, missing, onRetry }: LoadErrorProps) {
  const notFound = error.status === 404
  return (
    <Page>
      <Tile className="flex flex-col gap-3 sm:p-8" role="alert">
        <h1 className="text-2xl">{notFound ? 'This link does not open anything' : 'This page could not load'}</h1>
        <p className="max-w-prose text-ink-soft">{notFound ? missing : error.message}</p>
        <div className="flex flex-wrap gap-3 pt-2">
          {!notFound && <Button onClick={onRetry}>Try again</Button>}
          <ButtonLink to="/" variant="secondary">
            Go to the start page
          </ButtonLink>
        </div>
      </Tile>
    </Page>
  )
}

export function RefreshError({ polling, onRetry }: { polling: boolean; onRetry: () => void }) {
  return (
    <Notice tone="attention">
      {polling ? (
        'This page could not refresh. It will try again in a few seconds.'
      ) : (
        <span className="flex flex-wrap items-center gap-3">
          This page could not refresh.
          <Button variant="secondary" size="sm" onClick={onRetry}>
            Refresh
          </Button>
        </span>
      )}
    </Notice>
  )
}
