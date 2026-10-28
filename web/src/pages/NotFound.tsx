import { ButtonLink, Page } from '../components/ui'

export default function NotFound() {
  return (
    <Page>
      <title>Page not found – CommitPool</title>
      <div className="flex flex-col gap-4">
        <h1 className="text-3xl font-bold">There is no page here</h1>
        <p className="text-ink-soft">If someone sent you a link, check that you opened all of it.</p>
        <div>
          <ButtonLink to="/" variant="secondary">
            Go to the start page
          </ButtonLink>
        </div>
      </div>
    </Page>
  )
}
