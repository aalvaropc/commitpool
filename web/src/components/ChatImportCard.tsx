import { useEffect, useRef, useState, type FormEvent, type Ref } from 'react'
import { api, type ChatPreview, type OwnerView, type Reading } from '../api'
import { addLabel, chatLength, chatProblem, importedNote, MAX_CHAT_CHARS, personTag, progressText, readChatFile, readingNow, ticked } from '../lib/chat'
import { count } from '../lib/plural'
import { Button, Icon, Notice, Tile, inputClass } from './ui'

export const CHAT_HELP =
  "Paste the messages, or upload WhatsApp's Export chat (.txt, without media). CommitPool finds who said what; Claude reads each person against your dates."

export interface ChatCardState {
  text: string
  preview: ChatPreview | null
  chosen: string[]
  error: string | null
}

export type ChatBusy = 'read' | 'add' | 'file' | null

export function chatCard(text = ''): ChatCardState {
  return { text, preview: null, chosen: [], error: null }
}

export async function withFile(state: ChatCardState, file: File): Promise<ChatCardState> {
  try {
    return chatCard(await readChatFile(file))
  } catch (e) {
    return { ...state, error: e instanceof Error ? e.message : 'That file could not be read.' }
  }
}

export function withPreview(state: ChatCardState, preview: ChatPreview): ChatCardState {
  return { ...state, preview, chosen: ticked(preview.people), error: null }
}

const failure = (e: unknown) => (e instanceof Error ? e.message : 'Something went wrong. Please try again.')

interface PanelProps {
  state: ChatCardState
  busy: ChatBusy
  labelledBy: string
  onText: (text: string) => void
  onRead: () => void
  onFile: (file: File) => void
  onToggle: (name: string) => void
  onAdd: () => void
  textRef?: Ref<HTMLTextAreaElement>
}

function PreviewList({ preview, chosen, busy, onToggle, onAdd }: { preview: ChatPreview; chosen: string[]; busy: ChatBusy; onToggle: (name: string) => void; onAdd: () => void }) {
  return (
    <div className="flex flex-col gap-3">
      <fieldset className="flex min-w-0 flex-col gap-2">
        <legend className="mb-2 font-semibold">{`${count(preview.people.length, 'person', 'people')} in the chat`}</legend>
        <ul className="flex flex-col divide-y divide-line rounded-control ring-1 ring-line ring-inset">
          {preview.people.map((p) => {
            const tag = personTag(p)
            const locked = p.host || p.joined
            return (
              <li key={p.name}>
                <label className={`flex min-h-11 items-start gap-3 px-3.5 py-3 ${locked ? 'cursor-default' : 'cursor-pointer'}`}>
                  <input
                    type="checkbox"
                    checked={chosen.includes(p.name)}
                    disabled={locked || busy === 'add'}
                    onChange={() => onToggle(p.name)}
                    className="mt-0.5 size-5 shrink-0 accent-brand"
                  />
                  <span className="flex min-w-0 flex-col gap-0.5">
                    <span className={`font-semibold [overflow-wrap:anywhere] ${locked ? 'text-ink-soft' : ''}`}>
                      {p.name}
                      {tag && <span className="font-normal"> · {tag}</span>}
                    </span>
                    {p.excerpt && <span className="text-sm text-ink-soft [overflow-wrap:anywhere]">“{p.excerpt}”</span>}
                  </span>
                </label>
              </li>
            )
          })}
        </ul>
        {preview.skipped > 0 && <p className="text-sm text-ink-soft">{`${count(preview.skipped, 'line')} left out: system messages, media and deleted messages.`}</p>}
      </fieldset>
      <div>
        <Button onClick={onAdd} busy={busy === 'add'} busyLabel="Adding them…" disabled={chosen.length === 0}>
          {addLabel(chosen.length)}
          <Icon name="arrowRight" size={18} />
        </Button>
      </div>
    </div>
  )
}

export function ChatImportPanel({ state, busy, labelledBy, onText, onRead, onFile, onToggle, onAdd, textRef }: PanelProps) {
  const file = useRef<HTMLInputElement>(null)
  const length = chatLength(state.text)
  const over = length > MAX_CHAT_CHARS
  const invalid = state.error !== null && state.preview === null

  function submit(e: FormEvent) {
    e.preventDefault()
    onRead()
  }

  return (
    <div className="flex flex-col gap-4">
      <p id="chat-help" className="max-w-prose text-ink-soft">
        {CHAT_HELP}
      </p>
      <form onSubmit={submit} noValidate className="flex flex-col gap-2">
        <textarea
          id="chat-text"
          ref={textRef}
          value={state.text}
          onChange={(e) => onText(e.target.value)}
          rows={8}
          spellCheck={false}
          aria-labelledby={labelledBy}
          aria-describedby={invalid ? 'chat-help chat-count chat-error' : 'chat-help chat-count'}
          aria-invalid={invalid || over ? true : undefined}
          className={`${inputClass} resize-y text-sm leading-relaxed`}
        />
        <p id="chat-count" className={`text-sm tabular-nums ${over ? 'font-semibold text-no' : 'text-ink-soft'}`}>
          {`${length.toLocaleString('en-US')} of ${MAX_CHAT_CHARS.toLocaleString('en-US')} characters`}
        </p>
        <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap">
          <Button type="submit" busy={busy === 'read'} busyLabel="Reading the chat…" disabled={busy === 'add'}>
            Read the chat
          </Button>
          <Button variant="secondary" onClick={() => file.current?.click()} busy={busy === 'file'} busyLabel="Opening the file…" disabled={busy === 'add'}>
            <Icon name="plus" size={18} />
            Upload the export (.txt)
          </Button>
          <input
            ref={file}
            type="file"
            accept=".txt,text/plain"
            hidden
            onChange={(e) => {
              const picked = e.currentTarget.files?.[0]
              e.currentTarget.value = ''
              if (picked) onFile(picked)
            }}
          />
        </div>
      </form>
      {state.error && (
        <Notice tone="attention" role="alert">
          <span id="chat-error">{state.error}</span>
        </Notice>
      )}
      {state.preview && <PreviewList preview={state.preview} chosen={state.chosen} busy={busy} onToggle={onToggle} onAdd={onAdd} />}
    </div>
  )
}

export function ChatReading({ reading }: { reading?: Reading }) {
  const shown = readingNow(reading) && reading ? reading : null
  return (
    <div role="status" className="empty:hidden">
      {shown && (
        <div className="flex flex-col gap-2 rounded-tile bg-brand-wash px-4 py-3 sm:px-6">
          <p className="flex items-center gap-2.5 font-semibold text-ink">
            <Icon name="spark" size={20} className="shrink-0 text-brand motion-safe:animate-pulse" />
            {progressText(shown)}
          </p>
          <span aria-hidden="true" className="h-1.5 overflow-hidden rounded-full bg-surface">
            <span className="block h-full rounded-full bg-brand transition-[width] duration-500" style={{ width: `${Math.round(((shown.total - shown.pending) / shown.total) * 100)}%` }} />
          </span>
        </div>
      )}
    </div>
  )
}

interface Props {
  token: string
  view: OwnerView
  onImported: () => Promise<unknown>
}

export default function ChatImportCard({ token, view, onImported }: Props) {
  const fresh = view.participants.length === 0
  const [state, setState] = useState(() => chatCard(fresh ? (view.demo_chat ?? '') : ''))
  const [busy, setBusy] = useState<ChatBusy>(null)
  const [open, setOpen] = useState(false)
  const [note, setNote] = useState('')
  const text = useRef<HTMLTextAreaElement>(null)
  const summary = useRef<HTMLElement>(null)
  const added = useRef(false)

  const working = state.text !== '' || state.preview !== null || state.error !== null
  useEffect(() => {
    if (fresh) return
    if (added.current) {
      added.current = false
      summary.current?.focus()
    } else if (working) {
      setOpen(true)
    }
  }, [fresh])

  async function read() {
    const problem = chatProblem(state.text)
    if (problem) {
      setState((s) => ({ ...s, preview: null, chosen: [], error: problem }))
      text.current?.focus()
      return
    }
    setBusy('read')
    setNote('')
    try {
      const preview = await api.previewChat(token, state.text)
      setState((s) => withPreview(s, preview))
    } catch (e) {
      setState((s) => ({ ...s, preview: null, chosen: [], error: failure(e) }))
    } finally {
      setBusy(null)
    }
  }

  async function add() {
    if (!state.preview || state.chosen.length === 0) return
    setBusy('add')
    try {
      const res = await api.importChat(token, state.text, state.chosen)
      setState(chatCard())
      setOpen(false)
      setNote(importedNote(res))
      added.current = true
      await onImported()
    } catch (e) {
      setState((s) => ({ ...s, error: failure(e) }))
    } finally {
      setBusy(null)
    }
  }

  async function upload(f: File) {
    setBusy('file')
    setNote('')
    const next = await withFile(state, f)
    setState(next)
    setBusy(null)
    text.current?.focus()
  }

  const panel = (
    <ChatImportPanel
      state={state}
      busy={busy}
      labelledBy="chat-title"
      textRef={text}
      onText={(t) => setState(chatCard(t))}
      onRead={() => void read()}
      onFile={(f) => void upload(f)}
      onToggle={(name) => setState((s) => ({ ...s, chosen: s.chosen.includes(name) ? s.chosen.filter((n) => n !== name) : [...s.chosen, name] }))}
      onAdd={() => void add()}
    />
  )

  return (
    <Tile as="section" aria-labelledby="chat-title" className="flex flex-col gap-4 sm:p-6">
      {fresh ? (
        <>
          <h2 id="chat-title" className="text-xl">
            Paste your group chat
          </h2>
          {panel}
        </>
      ) : (
        <details open={open} onToggle={(e) => setOpen(e.currentTarget.open)} className="group">
          <summary
            ref={summary}
            className="-mx-2 flex min-h-11 w-fit cursor-pointer list-none items-center gap-2 rounded-control px-2 font-semibold text-brand [&::-webkit-details-marker]:hidden"
          >
            <Icon name="arrowRight" size={18} className="shrink-0 transition-transform duration-200 group-open:rotate-90" />
            <span id="chat-title">Paste more of the chat</span>
          </summary>
          <div className="mt-3">{panel}</div>
        </details>
      )}
      <div role="status" className="empty:hidden">
        {note && <p className="text-sm text-ink-soft">{note}</p>}
      </div>
    </Tile>
  )
}
