import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { api, ApiError, type CreateWorkshopResponse } from '../api'
import CopyField from '../components/CopyField'
import { Button, ButtonLink, Field, Notice, Page, describedBy, inputClass } from '../components/ui'
import { browserTimeZone, TIME_ZONES, windowLabel } from '../lib/time'
import { DATES_RULE, firstInvalid, serverField, validateWorkshop, type FormErrors, type WorkshopForm } from '../lib/workshopForm'

const WINDOWS = [1440, 2880, 60, 15, 5]
const DAY_MS = 86400000

type Form = WorkshopForm
type Errors = FormErrors

function isoDay(offsetDays: number): string {
  const d = new Date(Date.now() + offsetDays * DAY_MS)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function focusFirstError(errors: Errors) {
  const key = firstInvalid(errors)
  const target = key ? document.getElementById(`f-${key}`) : document.getElementById('f-form-error')
  target?.focus()
}

export default function NewWorkshop() {
  const zone = browserTimeZone()
  const zones = useMemo(() => (TIME_ZONES.includes(zone) ? TIME_ZONES : [zone, ...TIME_ZONES]), [zone])
  const [form, setForm] = useState<Form>({
    title: '',
    description: '',
    duration: '120',
    timezone: zone,
    minimum: '3',
    capacity: '8',
    price: '10.00',
    window: '1440',
    date1: isoDay(7),
    time1: '15:00',
    date2: isoDay(8),
    time2: '10:00',
  })
  const [errors, setErrors] = useState<Errors>({})
  const [busy, setBusy] = useState(false)
  const [created, setCreated] = useState<CreateWorkshopResponse | null>(null)
  const createdHeading = useRef<HTMLHeadingElement>(null)
  const [focusErrors, setFocusErrors] = useState(0)

  useEffect(() => {
    if (created) createdHeading.current?.focus()
  }, [created])

  useEffect(() => {
    if (focusErrors) focusFirstError(errors)
  }, [focusErrors])

  const set = (key: keyof Form) => (e: { target: { value: string } }) => setForm((f) => ({ ...f, [key]: e.target.value }))

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
      const price = form.price.trim()
      const res = await api.createWorkshop({
        title: form.title.trim(),
        description: form.description.trim(),
        duration_min: Number(form.duration),
        timezone: form.timezone,
        minimum: Number(form.minimum),
        capacity: Number(form.capacity),
        price: price.includes('.') ? price : `${price}.00`,
        currency: 'USD',
        payment_window_min: Number(form.window),
        dates,
      })
      setCreated(res)
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
    const ownerPath = new URL(created.owner_url).pathname
    return (
      <Page>
        <title>Workshop created – CommitPool</title>
        <div className="flex flex-col gap-8">
          <h1 ref={createdHeading} tabIndex={-1} className="text-3xl font-bold sm:text-4xl">
            {created.workshop.title} is ready
          </h1>
          <Notice tone="attention" title="Save your organizer link now" role="alert">
            It is the only way back to your organizer page and it will not be shown again. Anyone who has it can open payments.
          </Notice>
          <CopyField id="owner-url" label="Organizer link (keep it private)" value={created.owner_url} />
          <CopyField id="share-url" label="Link for participants" value={created.share_url} hint="Send this one to the people you want to invite." />
          <div>
            <ButtonLink to={ownerPath}>Open the organizer page</ButtonLink>
          </div>
        </div>
      </Page>
    )
  }

  const input = (key: keyof Form, hint = false) => ({
    id: `f-${key}`,
    value: form[key],
    onChange: set(key),
    'aria-invalid': errors[key] ? true : undefined,
    'aria-describedby': describedBy(`f-${key}`, hint, errors[key]),
    className: inputClass,
  })

  return (
    <Page>
      <title>Create a workshop – CommitPool</title>
      <form onSubmit={submit} noValidate className="flex flex-col gap-10">
        <div className="flex flex-col gap-2">
          <h1 className="text-3xl font-bold sm:text-4xl">Create a workshop</h1>
          <p className="max-w-prose text-ink-soft">Nothing is charged until you open payments for a date and enough people pay.</p>
        </div>

        <fieldset className="flex flex-col gap-5">
          <legend className="mb-4 text-xl font-bold">The workshop</legend>
          <Field id="f-title" label="Title" error={errors.title}>
            <input {...input('title')} maxLength={120} />
          </Field>
          <Field id="f-description" label="Description" hint="Optional. What people will learn and what to bring." error={errors.description}>
            <textarea {...input('description', true)} rows={3} maxLength={2000} />
          </Field>
          <Field id="f-duration" label="Duration in minutes" error={errors.duration}>
            <input {...input('duration')} type="number" inputMode="numeric" min={1} max={1440} step={1} className={`${inputClass} sm:max-w-40`} />
          </Field>
        </fieldset>

        <fieldset className="flex flex-col gap-5">
          <legend className="mb-4 text-xl font-bold">Money and spots</legend>
          <div className="grid gap-5 sm:grid-cols-3">
            <Field id="f-price" label="Price per person, USD" hint="Prices are in US dollars." error={errors.price}>
              <input {...input('price', true)} inputMode="decimal" autoComplete="off" />
            </Field>
            <Field id="f-minimum" label="People needed to happen" error={errors.minimum}>
              <input {...input('minimum')} type="number" inputMode="numeric" min={1} step={1} />
            </Field>
            <Field id="f-capacity" label="Spots" error={errors.capacity}>
              <input {...input('capacity')} type="number" inputMode="numeric" min={1} max={500} step={1} />
            </Field>
          </div>
          <Field id="f-window" label="Time to reserve" hint="How long people have to reserve once you open reservations. At most 3 days, the time PayPal guarantees a hold." error={errors.window}>
            <select {...input('window', true)} className={`${inputClass} sm:max-w-xs`}>
              {WINDOWS.map((m) => (
                <option key={m} value={m}>
                  {windowLabel(m)}
                </option>
              ))}
            </select>
          </Field>
        </fieldset>

        <fieldset className="flex flex-col gap-5">
          <legend className="mb-4 text-xl font-bold">Candidate dates</legend>
          <p id="dates-rule" className="max-w-prose text-ink-soft">
            {DATES_RULE} Reservations open when you choose a date, and the time to reserve runs from then.
          </p>
          <Field id="f-timezone" label="Time zone" hint="Everyone sees the dates and deadlines in this time zone." error={errors.timezone}>
            <select {...input('timezone', true)} className={`${inputClass} sm:max-w-sm`}>
              {zones.map((z) => (
                <option key={z} value={z}>
                  {z.replaceAll('_', ' ')}
                </option>
              ))}
            </select>
          </Field>
          {(
            [
              ['First date', 'date1', 'time1'],
              ['Second date', 'date2', 'time2'],
            ] as const
          ).map(([legend, dk, tk]) => (
            <fieldset key={dk} className="flex flex-col gap-2 border-t border-rule pt-4">
              <legend className="font-semibold">{legend}</legend>
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="flex flex-col gap-1.5">
                  <label htmlFor={`f-${dk}`} className="text-sm text-ink-soft">
                    Day
                  </label>
                  <input {...input(dk)} aria-describedby={[describedBy(`f-${dk}`, false, errors[dk]), 'dates-rule'].filter(Boolean).join(' ')} type="date" />
                </div>
                <div className="flex flex-col gap-1.5">
                  <label htmlFor={`f-${tk}`} className="text-sm text-ink-soft">
                    Starts at
                  </label>
                  <input {...input(tk)} aria-describedby={describedBy(`f-${dk}`, false, errors[dk])} type="time" />
                </div>
              </div>
              {errors[dk] && (
                <p id={`f-${dk}-error`} className="text-sm font-medium text-cancel">
                  {errors[dk]}
                </p>
              )}
            </fieldset>
          ))}
          <p className="text-sm text-ink-soft">Each date lasts the duration above.</p>
        </fieldset>

        <div className="flex flex-col items-start gap-4">
          {errors.form && (
            <div id="f-form-error" tabIndex={-1}>
              <Notice tone="attention" role="alert">
                {errors.form}
              </Notice>
            </div>
          )}
          {Object.keys(errors).some((k) => k !== 'form') && (
            <p role="alert" className="font-medium text-cancel">
              Some fields need a fix before the workshop can be created.
            </p>
          )}
          <Button type="submit" busy={busy} busyLabel="Creating…" className="px-6 py-3 text-lg">
            Create workshop
          </Button>
        </div>
      </form>
    </Page>
  )
}
