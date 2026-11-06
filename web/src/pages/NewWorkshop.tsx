import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { api, ApiError, type CreateWorkshopResponse } from '../api'
import DescribeWorkshop from '../components/DescribeWorkshop'
import { useDemoAvailable } from '../components/TryDemo'
import WorkshopCreated from '../components/WorkshopCreated'
import WorkshopReview, { type ReviewNote } from '../components/WorkshopReview'
import { Page } from '../components/ui'
import { browserTimeZone, TIME_ZONES } from '../lib/time'
import { blankForm, dateErrors, firstInvalid, fromDraft, serverField, toRequest, validateWorkshop, type DateSlot, type FieldKey, type FormErrors, type WorkshopForm } from '../lib/workshopForm'

type Step = 'describe' | 'review'

const DRAFTED: ReviewNote = { tone: 'hold', text: 'Claude drafted this from your description. Check every field; nothing is created until you press Create workshop.' }
const NOT_DRAFTED: ReviewNote = { tone: 'attention', text: 'Claude couldn’t draft this one right now. Fill in the form below; the dates in it are only suggestions.' }

function focusFirstError(errors: FormErrors, dates: DateSlot[]) {
  const key = firstInvalid(errors, dates)
  document.getElementById(key ? `f-${key}` : 'f-form-error')?.focus()
}

function withoutEdited(errors: FormErrors, key: FieldKey): FormErrors {
  const next = { ...errors }
  delete next[key]
  delete next.form
  if (key === 'dates' || key.startsWith('date:')) delete next.dates
  return next
}

export default function NewWorkshop() {
  const demo = useDemoAvailable()
  const zone = browserTimeZone()
  const [step, setStep] = useState<Step>('describe')
  const [text, setText] = useState('')
  const [drafting, setDrafting] = useState(false)
  const [draftError, setDraftError] = useState<string | null>(null)
  const [form, setForm] = useState<WorkshopForm>(() => blankForm(zone, Date.now()))
  const [missing, setMissing] = useState<FieldKey[]>([])
  const [note, setNote] = useState<ReviewNote | null>(null)
  const [errors, setErrors] = useState<FormErrors>({})
  const [busy, setBusy] = useState(false)
  const [created, setCreated] = useState<CreateWorkshopResponse | null>(null)
  const [focusErrors, setFocusErrors] = useState(0)
  const zones = useMemo(() => [...new Set([form.timezone, zone].filter((z) => !TIME_ZONES.includes(z)))].concat(TIME_ZONES), [form.timezone, zone])
  const reviewHeading = useRef<HTMLHeadingElement>(null)
  const cameBack = useRef(false)
  const draftRun = useRef(0)

  useEffect(() => {
    if (step === 'review') reviewHeading.current?.focus()
    else if (cameBack.current) document.getElementById('f-text')?.focus()
  }, [step])

  useEffect(() => {
    if (focusErrors) focusFirstError(errors, form.dates)
  }, [focusErrors])

  function review(next: WorkshopForm, gaps: FieldKey[], message: ReviewNote | null, found: FormErrors = {}) {
    setForm(next)
    setMissing(gaps)
    setNote(message)
    setErrors(found)
    setStep('review')
    window.scrollTo({ top: 0 })
  }

  async function draft() {
    const run = ++draftRun.current
    setDrafting(true)
    setDraftError(null)
    try {
      const res = await api.draftWorkshop(text.trim(), form.timezone)
      if (run !== draftRun.current) return
      const { form: next, missing: gaps } = fromDraft(res.draft, form)
      review(next, res.drafted ? gaps : [], res.drafted ? DRAFTED : NOT_DRAFTED, dateErrors(validateWorkshop(next, Date.now()).errors))
    } catch (err) {
      if (run !== draftRun.current) return
      setDraftError(err instanceof Error ? err.message : 'Claude could not draft it right now. Try again, or fill in the form yourself.')
    } finally {
      if (run === draftRun.current) setDrafting(false)
    }
  }

  function skip() {
    draftRun.current += 1
    setDrafting(false)
    review(form, [], null)
  }

  async function submit(e: FormEvent) {
    e.preventDefault()
    const { errors: found, dates } = validateWorkshop(form, Date.now())
    setErrors(found)
    if (!dates) {
      setFocusErrors((n) => n + 1)
      return
    }
    setBusy(true)
    try {
      setCreated(await api.createWorkshop(toRequest(form, dates)))
      window.scrollTo({ top: 0 })
    } catch (err) {
      const message = err instanceof Error ? err.message : 'The workshop could not be created.'
      const field = err instanceof ApiError ? serverField(err.code) : null
      setErrors(field ? { [field]: message } : { form: message })
      setFocusErrors((n) => n + 1)
    } finally {
      setBusy(false)
    }
  }

  if (created) {
    return (
      <Page>
        <WorkshopCreated created={created} />
      </Page>
    )
  }

  return (
    <Page>
      <title>Create a workshop – CommitPool</title>
      {step === 'describe' ? (
        <DescribeWorkshop
          showExample={demo === true}
          text={text}
          onText={setText}
          drafting={drafting}
          error={draftError}
          onDraft={draft}
          onSkip={skip}
        />
      ) : (
        <WorkshopReview
          form={form}
          onChange={(next, edited) => {
            setForm(next)
            if (edited) setErrors((e) => withoutEdited(e, edited))
          }}
          errors={errors}
          missing={missing}
          note={note}
          zones={zones}
          busy={busy}
          onSubmit={submit}
          onBack={() => {
            cameBack.current = true
            setStep('describe')
          }}
          headingRef={reviewHeading}
        />
      )}
    </Page>
  )
}
