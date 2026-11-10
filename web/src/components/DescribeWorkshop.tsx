import { useState, type FormEvent } from 'react'
import { Button, Field, Icon, Notice, Tile, describedBy, inputClass } from './ui'

export const EXAMPLE =
  'I’m Iris Calder. I run life drawing for beginners at The Long Room in Red Hook, Brooklyn. Two hours, $45 a person, the room and the model cost me $120 a session, and I have 6 easels. Saturday or Sunday next week, 10 to 12.'

interface Props {
  showExample: boolean
  text: string
  onText: (text: string) => void
  drafting: boolean
  error: string | null
  onDraft: () => void
  onSkip: () => void
}

export default function DescribeWorkshop({ showExample, text, onText, drafting, error, onDraft, onSkip }: Props) {
  const [empty, setEmpty] = useState(false)

  function submit(e: FormEvent) {
    e.preventDefault()
    if (!text.trim()) {
      setEmpty(true)
      document.getElementById('f-text')?.focus()
      return
    }
    setEmpty(false)
    onDraft()
  }

  const fieldError = empty ? 'Write a few words about your workshop first, or fill in the form yourself.' : undefined
  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-6">
      <header className="flex flex-col gap-2">
        <p className="text-sm font-semibold text-brand">Step 1 of 2</p>
        <h1 className="text-3xl sm:text-5xl">Create a workshop</h1>
        <p className="max-w-prose text-lg text-ink-soft">
          Tell it the way you’d tell a friend. Claude drafts the form, and you check every field before anything is created.
        </p>
      </header>
      <Tile className="flex flex-col gap-5 sm:p-6">
        <Field
          id="f-text"
          label="Your workshop, in your own words"
          hint="What it is, who runs it, where, how long, the price, what the place costs you, how many people fit, and a few dates that could work."
          error={fieldError}
        >
          <textarea
            id="f-text"
            value={text}
            onChange={(e) => onText(e.target.value)}
            rows={7}
            maxLength={2000}
            aria-invalid={fieldError ? true : undefined}
            aria-describedby={describedBy('f-text', true, fieldError)}
            className={`${inputClass} leading-relaxed`}
          />
        </Field>
        <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap">
          <Button type="submit" size="lg" busy={drafting} busyLabel="Claude is drafting…">
            Draft it with Claude
            <Icon name="arrowRight" size={20} />
          </Button>
          {showExample && (
            <Button variant="secondary" size="lg" onClick={() => onText(EXAMPLE)} disabled={drafting}>
              Use Iris’s example
            </Button>
          )}
        </div>
        {error && (
          <Notice tone="attention" role="alert">
            {error}
          </Notice>
        )}
        <p className="text-sm text-ink-soft">Claude only drafts. Nothing is saved or shared until you press Create workshop.</p>
      </Tile>
      <p className="flex flex-wrap items-center gap-x-1 text-ink-soft">
        Rather fill in every field yourself?
        <Button variant="quiet" size="sm" onClick={onSkip}>
          Fill in the form myself
        </Button>
      </p>
    </form>
  )
}
