import { Link, Outlet } from 'react-router'
import { Logo } from './ui'

export default function Shell() {
  return (
    <div className="flex min-h-dvh flex-col">
      <a
        href="#main"
        className="sr-only rounded-control bg-brand px-3 py-2 font-semibold text-on-brand focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-50"
      >
        Skip to content
      </a>
      <header>
        <div className="mx-auto flex h-16 w-full max-w-6xl items-center justify-between gap-3 px-4 sm:px-6">
          <Link to="/" className="rounded-control no-underline" aria-label="CommitPool home">
            <Logo />
          </Link>
          <span
            className="rounded-full bg-surface-muted px-3 py-1.5 text-sm font-semibold whitespace-nowrap text-ink-soft"
            title="Payments use PayPal's test environment: no real money moves."
          >
            Test mode<span className="hidden sm:inline"> · PayPal Sandbox</span>
          </span>
        </div>
      </header>
      <main id="main" className="flex-1">
        <Outlet />
      </main>
      <footer className="mt-16">
        <div className="mx-auto flex w-full max-w-6xl flex-col gap-1 px-4 py-10 text-sm text-ink-soft sm:flex-row sm:items-center sm:justify-between sm:px-6">
          <p className="font-display text-base font-bold text-ink">The class happens, or nobody pays.</p>
          <p>Payments by PayPal · Reading by Claude on Amazon Bedrock</p>
        </div>
      </footer>
    </div>
  )
}
