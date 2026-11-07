import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router'
import TryDemo from '../TryDemo'
import { Button, Icon } from '../ui'

export default function StartSentence({ demo }: { demo: boolean | undefined }) {
  const navigate = useNavigate()
  const [text, setText] = useState('')

  function submit(e: FormEvent) {
    e.preventDefault()
    navigate('/new', { state: { text: text.trim() } })
  }

  return (
    <section aria-labelledby="start-title" className="rounded-[2rem] bg-brand p-6 text-on-brand sm:p-10 [&_:focus-visible]:outline-on-brand">
      <div className="flex flex-col gap-8">
        <h2 id="start-title" className="shout max-w-[14ch] text-display">
          Your class. A paid-for room.
        </h2>
        <form onSubmit={submit} className="flex flex-col gap-3 sm:flex-row sm:items-stretch">
          <label htmlFor="start-text" className="sr-only">
            Describe your class in one sentence
          </label>
          <input
            id="start-text"
            value={text}
            onChange={(e) => setText(e.target.value)}
            maxLength={600}
            autoComplete="off"
            placeholder="Wheel-throwing for beginners in Brooklyn, $45, two Saturdays"
            className="min-h-14 flex-1 rounded-full bg-canvas px-6 text-lg text-ink transition-colors duration-150 placeholder:text-ink-faint hover:bg-surface focus-visible:bg-surface"
          />
          <Button type="submit" size="lg" variant="inverse">
            Draft it with Claude
            <Icon name="arrowRight" size={20} />
          </Button>
        </form>
        <p className="max-w-[60ch] text-lg font-medium">
          Claude drafts the form from your sentence. Nothing is created until you check it and press Create workshop.
        </p>
        {demo !== false && (
          <div className="w-fit">
            <TryDemo pending={demo === undefined} />
          </div>
        )}
      </div>
    </section>
  )
}
