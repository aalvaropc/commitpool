import { useRef, useState } from 'react'
import { Button, inputClass } from './ui'

interface Props {
  id: string
  label: string
  value: string
  hint?: string
  multiline?: boolean
}

export default function CopyField({ id, label, value, hint, multiline = false }: Props) {
  const field = useRef<HTMLInputElement & HTMLTextAreaElement>(null)
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle')
  const what = multiline ? 'message' : 'link'

  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setState('copied')
    } catch {
      field.current?.select()
      setState('failed')
    }
  }

  const shared = {
    id,
    ref: field,
    readOnly: true,
    value,
    'aria-describedby': hint ? `${id}-hint` : undefined,
    onFocus: (e: { currentTarget: { select: () => void } }) => e.currentTarget.select(),
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
      <div className={`flex flex-col gap-2 ${multiline ? '' : 'sm:flex-row'}`}>
        {multiline ? (
          <textarea {...shared} rows={5} className={`${inputClass} min-w-0 resize-none leading-relaxed`} />
        ) : (
          <input {...shared} className={`${inputClass} min-w-0 text-sm`} />
        )}
        <Button variant="secondary" onClick={copy} aria-label={`Copy ${what}: ${label}`} className="shrink-0 self-start">
          Copy {what}
        </Button>
      </div>
      <p role="status" className="min-h-5 text-sm text-ink-soft">
        {state === 'copied'
          ? `${what === 'link' ? 'Link' : 'Message'} copied.`
          : state === 'failed'
            ? `Copying is blocked here. The ${what} is selected; copy it with Ctrl+C or ⌘C.`
            : ''}
      </p>
    </div>
  )
}
