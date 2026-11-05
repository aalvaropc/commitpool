import { Link, Outlet } from 'react-router'

export default function Shell() {
  return (
    <div className="flex min-h-dvh flex-col">
      <a
        href="#main"
        className="sr-only rounded-md bg-signal px-3 py-2 font-semibold text-white focus:not-sr-only focus:absolute focus:top-2 focus:left-2"
      >
        Skip to content
      </a>
      <header className="border-b border-rule">
        <div className="mx-auto flex h-14 w-full max-w-6xl items-center px-4 sm:px-6">
          <Link to="/" className="wide text-lg font-bold tracking-tight text-ink no-underline">
            CommitPool
          </Link>
          <span className="ml-3 rounded-sm border border-dashed border-edge px-2 py-0.5 text-xs font-semibold text-ink-soft" title="Payments use PayPal's test environment: no real money moves.">
            PayPal Sandbox
          </span>
        </div>
      </header>
      <main id="main" className="flex-1">
        <Outlet />
      </main>
      <footer className="border-t border-rule">
        <p className="mx-auto w-full max-w-6xl px-4 py-6 text-sm text-ink-soft sm:px-6">
          Payments run through PayPal. A hold is charged only when the group can reach its minimum.
        </p>
      </footer>
    </div>
  )
}
