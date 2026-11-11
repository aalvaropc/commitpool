import { Link, Outlet, useLocation } from 'react-router'
import { REPO_URL, safeHttps } from '../lib/links'
import { useDemoInfo } from '../lib/demoInfo'
import ExternalLink from './ExternalLink'
import TryDemo, { useDemoAvailable } from './TryDemo'
import { ButtonLink, Logo } from './ui'

const sections = [
  { href: '#see', label: 'See it work' },
  { href: '#money', label: 'The money' },
  { href: '#paypal', label: 'PayPal' },
  { href: '#judges', label: 'For judges' },
  { href: '#faq', label: 'FAQ' },
]

function TestMode({ landing }: { landing: boolean }) {
  return (
    <span
      className={`rounded-full bg-surface-muted px-3 py-1.5 text-sm font-semibold whitespace-nowrap text-ink-soft ${landing ? 'hidden xl:inline' : ''}`}
      title="Payments use PayPal's test environment: no real money moves."
    >
      Test mode<span className={landing ? '' : 'hidden sm:inline'}> · PayPal Sandbox</span>
    </span>
  )
}

function LandingAction() {
  const demo = useDemoAvailable()
  if (demo === false) {
    return (
      <ButtonLink to="/new" size="sm">
        Create a workshop
      </ButtonLink>
    )
  }
  return <TryDemo size="sm" pending={demo === undefined} floatingError />
}

function FooterLinks() {
  const devpost = safeHttps(useDemoInfo()?.devpost_url)
  const link = 'inline-flex min-h-11 items-center gap-1.5 rounded-control font-semibold text-ink-soft underline-offset-4 hover:text-ink'
  return (
    <nav aria-label="Project links" className="flex flex-wrap gap-x-5">
      <ExternalLink href={REPO_URL} className={link}>
        Code on GitHub
      </ExternalLink>
      {devpost && (
        <ExternalLink href={devpost} className={link}>
          Devpost
        </ExternalLink>
      )}
    </nav>
  )
}

export default function Shell() {
  const landing = useLocation().pathname === '/'
  return (
    <div className="flex min-h-dvh flex-col">
      <a
        href="#main"
        className="sr-only rounded-control bg-brand px-3 py-2 font-semibold text-on-brand focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-50"
      >
        Skip to content
      </a>
      <header className="relative z-10">
        <div className={landing ? 'wrap flex min-h-19 items-center justify-between gap-4' : 'mx-auto flex h-16 w-full max-w-6xl items-center justify-between gap-3 px-4 sm:px-6'}>
          <Link to="/" className="inline-flex min-h-11 items-center rounded-control no-underline" aria-label="CommitPool home">
            {landing ? <Logo size="text-[1.375rem]" mark={30} /> : <Logo />}
          </Link>
          {landing && (
            <nav aria-label="Sections" className="hidden items-center gap-5 text-[0.9375rem] whitespace-nowrap text-ink-soft lg:flex xl:gap-7">
              {sections.map((s) => (
                <a key={s.href} href={s.href} className="inline-flex min-h-11 min-w-11 items-center justify-center rounded-control no-underline transition-colors duration-150 hover:text-ink">
                  {s.label}
                </a>
              ))}
            </nav>
          )}
          <div className="flex items-center gap-3">
            <TestMode landing={landing} />
            {landing && <LandingAction />}
          </div>
        </div>
      </header>
      <main id="main" className="flex-1">
        <Outlet />
      </main>
      {landing ? (
        <footer>
          <div className="wrap flex flex-col gap-4 pt-9 pb-11 text-sm text-ink-faint sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
            <Logo size="text-lg" mark={24} />
            <FooterLinks />
            <p>Payments by PayPal · Reading by Claude on Amazon Bedrock · Sandbox only, no real money</p>
          </div>
        </footer>
      ) : (
        <footer className="mt-16">
          <div className="mx-auto flex w-full max-w-6xl flex-col gap-1 px-4 py-10 text-sm text-ink-soft sm:flex-row sm:items-center sm:justify-between sm:px-6">
            <p className="font-display text-base font-bold text-ink">
              Find the date that fills the class. <span className="font-normal text-ink-soft">The class happens, or nobody pays.</span>
            </p>
            <p>Payments by PayPal · Reading by Claude on Amazon Bedrock</p>
          </div>
        </footer>
      )}
    </div>
  )
}
