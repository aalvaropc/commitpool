import { useEffect, useRef, useState, type FormEvent, type Ref } from 'react'
import { api, type ChatPreview, type OwnerView, type Reading } from '../api'
import { addLabel, chatLength, chatProblem, chosenCounts, importedNote, isLocked, MAX_CHAT_CHARS, personTag, progressText, readChatFile, readingNow, ticked } from '../lib/chat'
import { chatLead, updateLead } from '../lib/demo'
import { count } from '../lib/plural'
import { Button, Icon, Notice, Tile, inputClass } from './ui'

export const CHAT_HELP =
  "Paste the messages, or upload WhatsApp’s Export chat (.txt, without media). CommitPool finds who said what; Claude reads each person against your dates."

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
  lead?: string | null
  onText: (text: string) => void
  onRead: () => void
  onFile: (file: File) => void
  onToggle: (name: string) => void
  onAdd: () => void
  textRef?: Ref<HTMLTextAreaElement>
  foundRef?: Ref<HTMLHeadingElement>
}

function PreviewList({
  preview,
  chosen,
  busy,
  foundRef,
  onToggle,
  onAdd,
}: {
  preview: ChatPreview
  chosen: string[]
  busy: ChatBusy
  foundRef?: Ref<HTMLHeadingElement>
  onToggle: (name: string) => void
  onAdd: () => void
}) {
  const picked = chosenCounts(preview.people, chosen)
  const onlyUpdates = picked.add === 0 && picked.update > 0
  return (
    <div className="flex flex-col gap-3">
      <h3 id="chat-found" ref={foundRef} tabIndex={-1} className="scroll-mt-4 font-sans text-base font-semibold">
        {`${count(preview.people.length, 'person', 'people')} in the chat`}
      </h3>
      <fieldset aria-labelledby="chat-found" className="flex min-w-0 flex-col gap-2">
        <ul className="flex flex-col divide-y divide-line rounded-control ring-1 ring-line ring-inset">
          {preview.people.map((p) => {
            const tag = personTag(p)
            const locked = isLocked(p)
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
      <div className="flex flex-col sm:block">
        <Button onClick={onAdd} busy={busy === 'add'} busyLabel={onlyUpdates ? 'Sending them to Claude…' : 'Adding them…'} disabled={chosen.length === 0}>
          {addLabel(picked.add, picked.update)}
          <Icon name="arrowRight" size={18} />
        </Button>
      </div>
    </div>
  )
}

export function ChatImportPanel({ state, busy, labelledBy, lead, onText, onRead, onFile, onToggle, onAdd, textRef, foundRef }: PanelProps) {
  const file = useRef<HTMLInputElement>(null)
  const length = chatLength(state.text)
  const over = length > MAX_CHAT_CHARS
  const invalid = state.error !== null && state.preview === null

  function submit(e: FormEvent) {
    e.preventDefault()
    onRead()
  }

  return (
    <div className="flex flex-col gap-3 sm:gap-4">
      {lead && (
        <p className="flex items-start gap-2 rounded-control bg-brand-wash px-3.5 py-2.5 text-sm font-semibold text-ink sm:text-base">
          <Icon name="spark" size={18} className="mt-0.5 shrink-0 text-brand" />
          <span>
            <span className="sr-only">Next in the demo: </span>
            {lead}
          </span>
        </p>
      )}
      <p id="chat-help" className="max-w-prose text-ink-soft max-sm:order-last max-sm:text-sm">
        {CHAT_HELP}
      </p>
      <form onSubmit={submit} noValidate className="flex flex-col gap-2">
        <textarea
          id="chat-text"
          ref={textRef}
          value={state.text}
          onChange={(e) => onText(e.target.value)}
          rows={6}
          spellCheck={false}
          disabled={busy === 'file'}
          placeholder={'Ana: Saturday works for me\nBen: Sunday, if we finish by noon'}
          aria-labelledby={labelledBy}
          aria-describedby={invalid ? 'chat-help chat-count chat-error' : 'chat-help chat-count'}
          aria-invalid={invalid || over ? true : undefined}
          className={`${inputClass} resize-y text-base leading-relaxed sm:text-sm`}
        />
        <p id="chat-count" className={`text-sm tabular-nums ${over ? 'font-semibold text-no' : 'text-ink-soft'}`}>
          {`${length.toLocaleString('en-US')} of ${MAX_CHAT_CHARS.toLocaleString('en-US')} characters`}
        </p>
        <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap">
          <Button type="submit" busy={busy === 'read'} busyLabel="Reading the chat…" disabled={busy === 'add' || busy === 'file'}>
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
      {state.preview && <PreviewList preview={state.preview} chosen={state.chosen} busy={busy} foundRef={foundRef} onToggle={onToggle} onAdd={onAdd} />}
    </div>
  )
}

export function ChatReading({ reading }: { reading?: Reading }) {
  const shown = readingNow(reading) && reading ? reading : null
  return (
    <div role="status" className="empty:hidden">
      {shown && (
        <div id="chat-reading" className="flex scroll-mt-4 flex-col gap-2 rounded-tile bg-brand-wash px-4 py-3 sm:px-6">
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
  const [state, setState] = useState(() => chatCard((fresh ? view.demo_chat : view.demo_update) ?? ''))
  const [busy, setBusy] = useState<ChatBusy>(null)
  const [open, setOpen] = useState(false)
  const [note, setNote] = useState('')
  const text = useRef<HTMLTextAreaElement>(null)
  const found = useRef<HTMLHeadingElement>(null)
  const after = useRef<'found' | 'text' | null>(null)
  const wasFresh = useRef(fresh)
  const working = state.text !== '' || state.preview !== null || state.error !== null
  const lead = view.workshop.demo ? (fresh ? chatLead(state.text, view.demo_chat) : updateLead(state.text, view.demo_update)) : null

  useEffect(() => {
    if (wasFresh.current && !fresh && working) setOpen(true)
    wasFresh.current = fresh
  }, [fresh])

  useEffect(() => {
    if (busy !== null || after.current === null) return
    const target = after.current
    after.current = null
    if (target === 'found' && found.current) {
      found.current.focus({ preventScroll: true })
      found.current.scrollIntoView({ block: 'start' })
    }
    if (target === 'text') text.current?.focus()
  }, [busy, state])

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
      after.current = 'found'
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
    after.current = 'text'
    setBusy(null)
  }

  const panel = (
    <ChatImportPanel
      state={state}
      busy={busy}
      labelledBy="chat-title"
      lead={lead}
      textRef={text}
      foundRef={found}
      onText={(t) => setState(chatCard(t))}
      onRead={() => void read()}
      onFile={(f) => void upload(f)}
      onToggle={(name) => setState((s) => ({ ...s, chosen: s.chosen.includes(name) ? s.chosen.filter((n) => n !== name) : [...s.chosen, name] }))}
      onAdd={() => void add()}
    />
  )

  return (
    <Tile as="section" aria-labelledby="chat-title" className="flex flex-col gap-3 sm:gap-4 sm:p-6">
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
