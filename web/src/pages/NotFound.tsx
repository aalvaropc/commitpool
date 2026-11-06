import { ButtonLink, Page, Tile } from '../components/ui'

export default function NotFound() {
  return (
    <Page>
      <title>Page not found – CommitPool</title>
      <Tile className="flex flex-col items-start gap-4 sm:p-8">
        <h1 className="text-3xl">There is no page here</h1>
        <p className="text-ink-soft">If someone sent you a link, check that you opened all of it.</p>
        <ButtonLink to="/" variant="secondary">
          Go to the start page
        </ButtonLink>
      </Tile>
    </Page>
  )
}
