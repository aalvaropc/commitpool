import { useRef, useState } from 'react'
import { Button, inputClass } from './ui'

interface Props {
  id: string
  label: string
  value: string
  hint?: string
}

/** A link to pass on, shown whole, with a button that copies it. */
export default function CopyField({ id, label, value, hint }: Props) {
  const input = useRef<HTMLInputElement>(null)
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle')

  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setState('copied')
    } catch {
      input.current?.select()
      setState('failed')
    }
  }

  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={id} className="font-semibold">
        {label}
      </label>
      {hint && (
        <p id={`${id}-hint`} className="text-sm text-ink-soft">
          {hint}
        </p>
      )}
      <div className="flex flex-col gap-2 sm:flex-row">
        <input
          id={id}
          ref={input}
          readOnly
          value={value}
          aria-describedby={hint ? `${id}-hint` : undefined}
          onFocus={(e) => e.currentTarget.select()}
          className={`${inputClass} min-w-0 text-sm`}
        />
        <Button variant="secondary" onClick={copy} aria-label={`Copy link: ${label}`} className="shrink-0">
          Copy link
        </Button>
      </div>
      <p role="status" className="min-h-5 text-sm text-ink-soft">
        {state === 'copied' ? 'Link copied.' : state === 'failed' ? 'Copying is blocked here. The link is selected; copy it with Ctrl+C or ⌘C.' : ''}
      </p>
    </div>
  )
}
