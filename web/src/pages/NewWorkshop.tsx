import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { api, ApiError, type CreateWorkshopResponse } from '../api'
import DescribeWorkshop from '../components/DescribeWorkshop'
import WorkshopCreated from '../components/WorkshopCreated'
import WorkshopReview, { type ReviewNote } from '../components/WorkshopReview'
import { Page } from '../components/ui'
import { browserTimeZone, TIME_ZONES } from '../lib/time'
import { blankForm, firstInvalid, fromDraft, serverField, toRequest, validateWorkshop, type FieldKey, type FormErrors, type WorkshopForm } from '../lib/workshopForm'

type Step = 'describe' | 'review'

const DRAFTED: ReviewNote = { tone: 'hold', text: 'Claude drafted this from your description. Check every field; nothing is created until you press Create workshop.' }
const NOT_DRAFTED: ReviewNote = { tone: 'attention', text: 'Claude couldn’t draft this one right now, so the form starts empty. Fill it in below.' }

function focusFirstError(errors: FormErrors) {
  const key = firstInvalid(errors)
  document.getElementById(key ? `f-${key}` : 'f-form-error')?.focus()
}

export default function NewWorkshop() {
  const zone = browserTimeZone()
  const zones = useMemo(() => (TIME_ZONES.includes(zone) ? TIME_ZONES : [zone, ...TIME_ZONES]), [zone])
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
  const reviewHeading = useRef<HTMLHeadingElement>(null)
  const cameBack = useRef(false)

  useEffect(() => {
    if (step === 'review') reviewHeading.current?.focus()
    else if (cameBack.current) document.getElementById('f-text')?.focus()
  }, [step])

  useEffect(() => {
    if (focusErrors) focusFirstError(errors)
  }, [focusErrors])

  function review(next: WorkshopForm, gaps: FieldKey[], message: ReviewNote | null) {
    setForm(next)
    setMissing(gaps)
    setNote(message)
    setErrors({})
    setStep('review')
    window.scrollTo({ top: 0 })
  }

  async function draft() {
    setDrafting(true)
    setDraftError(null)
    try {
      const res = await api.draftWorkshop(text.trim(), form.timezone)
      const base = blankForm(form.timezone, Date.now())
      const { form: next, missing: gaps } = fromDraft(res.draft, base)
      review(next, res.drafted ? gaps : [], res.drafted ? DRAFTED : NOT_DRAFTED)
    } catch (err) {
      setDraftError(err instanceof Error ? err.message : 'Claude could not draft it right now. Try again, or fill in the form yourself.')
    } finally {
      setDrafting(false)
    }
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
          text={text}
          onText={setText}
          drafting={drafting}
          error={draftError}
          onDraft={draft}
          onSkip={() => review(form, [], null)}
        />
      ) : (
        <WorkshopReview
          form={form}
          onChange={setForm}
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
