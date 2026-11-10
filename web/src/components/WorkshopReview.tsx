import { useEffect, useRef, type FormEvent, type ReactNode, type Ref } from 'react'
import { windowLabel } from '../lib/time'
import { breakEven, DATES_RULE, fitsBreakEven, MAX_DATES, newSlot, nextDay, type DateSlot, type FieldKey, type FormErrors, type TextField, type WorkshopForm } from '../lib/workshopForm'
import { Button, Field, Icon, Notice, Tile, describedBy, inputClass } from './ui'

const WINDOWS = [1440, 2880, 60, 15, 5]
const NEEDED: FieldKey[] = ['title', 'price', 'minimum', 'capacity', 'duration', 'dates']
const PAYOUT_HINT = 'Optional. When the class starts, CommitPool sends what people paid, less PayPal’s fees, to this PayPal account.'


export interface ReviewNote {
  tone: 'hold' | 'attention'
  text: string
}

interface Props {
  form: WorkshopForm
  onChange: (form: WorkshopForm, edited?: FieldKey) => void
  errors: FormErrors
  missing: FieldKey[]
  note: ReviewNote | null
  zones: string[]
  busy: boolean
  onSubmit: (e: FormEvent) => void
  onBack: () => void
  headingRef: Ref<HTMLHeadingElement>
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Tile className="sm:p-6">
      <fieldset className="flex min-w-0 flex-col gap-5">
        <legend className="mb-4 font-display text-xl font-bold">{title}</legend>
        {children}
      </fieldset>
    </Tile>
  )
}

export default function WorkshopReview({ form, onChange, errors, missing, note, zones, busy, onSubmit, onBack, headingRef }: Props) {
  const unread = (key: FieldKey) => missing.includes(key) && NEEDED.includes(key)
  const hint = (key: FieldKey, text?: string): ReactNode => {
    if (!unread(key) && !text) return undefined
    return (
      <>
        {unread(key) && <span className="font-semibold text-attention">Claude couldn’t tell from your description. </span>}
        {text}
      </>
    )
  }
  const focusNext = useRef<string | null>(null)
  useEffect(() => {
    if (!focusNext.current) return
    document.getElementById(focusNext.current)?.focus()
    focusNext.current = null
  })
  const set = (key: TextField) => (e: { target: { value: string } }) => onChange({ ...form, [key]: e.target.value }, key)
  const input = (key: TextField, text?: string) => ({
    id: `f-${key}`,
    value: form[key],
    onChange: set(key),
    'aria-invalid': errors[key] ? true : undefined,
    'aria-describedby': describedBy(`f-${key}`, hint(key, text) !== undefined, errors[key]),
    className: inputClass,
  })
  const setDate = (id: string, part: 'date' | 'time', value: string) =>
    onChange({ ...form, dates: form.dates.map((d) => (d.id === id ? { ...d, [part]: value } : d)) }, `date:${id}`)
  const addDate = () => {
    const last = form.dates.at(-1)
    const slot = newSlot(last ? nextDay(last.date) : '', last?.time ?? '10:00')
    focusNext.current = `f-date:${slot.id}`
    onChange({ ...form, dates: [...form.dates, slot] }, 'dates')
  }
  const removeDate = (slot: DateSlot) => {
    const i = form.dates.indexOf(slot)
    const neighbour = form.dates[i + 1] ?? form.dates[i - 1]
    focusNext.current = neighbour ? `f-date:${neighbour.id}` : 'f-dates'
    onChange({ ...form, dates: form.dates.filter((d) => d.id !== slot.id) }, `date:${slot.id}`)
  }
  const even = breakEven(form.price, form.venueCost)
  const applyEven = (seats: number) => {
    focusNext.current = 'f-minimum'
    onChange({ ...form, minimum: String(seats) }, 'minimum')
  }

  return (
    <form onSubmit={onSubmit} noValidate className="flex flex-col gap-5">
      <header className="flex flex-col gap-3">
        <p className="text-sm font-semibold text-brand">Step 2 of 2</p>
        <h1 ref={headingRef} tabIndex={-1} className="text-3xl sm:text-5xl">
          Check your workshop
        </h1>
        {note && (
          <Notice tone={note.tone} role={note.tone === 'attention' ? 'alert' : 'status'}>
            {note.text}
          </Notice>
        )}
        <div>
          <Button variant="quiet" size="sm" onClick={onBack} className="-ml-3">
            Back to the description
          </Button>
        </div>
      </header>

      <Section title="What">
        <Field id="f-title" label="Title" hint={hint('title')} error={errors.title}>
          <input {...input('title')} maxLength={120} />
        </Field>
        <Field id="f-description" label="Description" hint={hint('description', 'Optional. What people will do and anything to bring.')} error={errors.description}>
          <textarea {...input('description', 'Optional. What people will do and anything to bring.')} rows={3} maxLength={2000} />
        </Field>
      </Section>

      <Section title="Who and where">
        <div className="grid gap-5 sm:grid-cols-2">
          <Field id="f-hostName" label="Your name" hint="Shown on the invite as the host." error={errors.hostName}>
            <input {...input('hostName', 'Shown on the invite as the host.')} maxLength={80} autoComplete="name" />
          </Field>
          <Field id="f-location" label="Place" hint="Such as a venue and its neighbourhood." error={errors.location}>
            <input {...input('location', 'Such as a venue and its neighbourhood.')} maxLength={160} />
          </Field>
        </div>
        <Field id="f-ownerEmail" label="Your email" hint="Optional. We send you your organizer link and tell you when it’s on. Nobody else sees it." error={errors.ownerEmail}>
          <input {...input('ownerEmail', 'Optional. We send you your organizer link and tell you when it’s on. Nobody else sees it.')} type="email" inputMode="email" autoComplete="email" maxLength={254} />
        </Field>
      </Section>

      <Section title="Money and people">
        <div className="grid gap-5 sm:grid-cols-2">
          <Field id="f-price" label="Price per person, USD" hint={hint('price')} error={errors.price}>
            <input {...input('price')} inputMode="decimal" autoComplete="off" />
          </Field>
          <Field id="f-venueCost" label="What the session costs you (room, model, materials)" hint="Optional, in USD. Only you see it; it tells you how many people you need." error={errors.venueCost}>
            <input {...input('venueCost', 'Optional. Only you see it; it tells you how many people you need.')} inputMode="decimal" autoComplete="off" />
          </Field>
        </div>
        {even &&
          (fitsBreakEven(even.seats, form.capacity) ? (
            <div className="flex flex-col gap-3 rounded-control bg-paid-wash p-4 sm:flex-row sm:items-center sm:justify-between">
              <p className="font-semibold text-paid">{even.text}.</p>
              {form.minimum !== String(even.seats) && (
                <Button variant="secondary" size="sm" onClick={() => applyEven(even.seats)}>
                  Set people needed to {even.seats}
                </Button>
              )}
            </div>
          ) : (
            <p className="rounded-control bg-attention-wash p-4 font-semibold text-attention">
              Covering the session costs takes {even.seats} people, more than the spots you offer. Raise the price, add spots or check the session cost.
            </p>
          ))}
        <div className="grid gap-5 sm:grid-cols-2">
          <Field id="f-minimum" label="People needed to happen" hint={hint('minimum', 'If fewer reserve by the deadline, nobody pays.')} error={errors.minimum}>
            <input {...input('minimum', 'If fewer reserve by the deadline, nobody pays.')} type="number" inputMode="numeric" min={1} step={1} />
          </Field>
          <Field id="f-capacity" label="Spots" hint={hint('capacity', 'The most people who can take part.')} error={errors.capacity}>
            <input {...input('capacity', 'The most people who can take part.')} type="number" inputMode="numeric" min={1} max={500} step={1} />
          </Field>
        </div>
        <Field id="f-payoutEmail" label="PayPal email for your payout" hint={PAYOUT_HINT} error={errors.payoutEmail}>
          <input {...input('payoutEmail', PAYOUT_HINT)} type="email" inputMode="email" autoComplete="email" maxLength={254} />
        </Field>
      </Section>

      <Section title="When">
        <div className="grid gap-5 sm:grid-cols-2">
          <Field id="f-duration" label="Duration in minutes" hint={hint('duration')} error={errors.duration}>
            <input {...input('duration')} type="number" inputMode="numeric" min={1} max={1440} step={1} />
          </Field>
          <Field id="f-timezone" label="Time zone" hint="Everyone sees the dates in this time zone." error={errors.timezone}>
            <select {...input('timezone', 'Everyone sees the dates in this time zone.')}>
              {zones.map((z) => (
                <option key={z} value={z}>
                  {z.replaceAll('_', ' ')}
                </option>
              ))}
            </select>
          </Field>
        </div>
        <div id="f-dates" tabIndex={-1} className="flex flex-col gap-3 rounded-control">
          <p className="font-semibold">Dates that could work</p>
          <p id="dates-rule" className="text-sm text-ink-soft">
            {unread('dates') && <span className="font-semibold text-attention">Claude couldn’t find dates, so these are suggestions. </span>}
            {DATES_RULE} You pick one of them later, when you open reservations.
          </p>
          {errors.dates && (
            <p role="alert" className="text-sm font-medium text-no">
              {errors.dates}
            </p>
          )}
          <ol className="flex flex-col gap-3">
            {form.dates.map((slot, i) => {
              const error = errors[`date:${slot.id}`]
              const day = `f-date:${slot.id}`
              const time = `f-time:${slot.id}`
              return (
                <li key={slot.id}>
                  <fieldset className="flex flex-col gap-2 rounded-control p-3 ring-1 ring-line ring-inset">
                    <legend className="sr-only">Date {i + 1}</legend>
                    <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] items-end gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,10rem)_auto]">
                      <div className="flex flex-col gap-1.5">
                        <label htmlFor={day} className="text-sm text-ink-soft">
                          Day {i + 1}
                        </label>
                        <input
                          id={day}
                          type="date"
                          value={slot.date}
                          onChange={(e) => setDate(slot.id, 'date', e.target.value)}
                          aria-invalid={error ? true : undefined}
                          aria-describedby={[error ? `${day}-error` : '', 'dates-rule'].filter(Boolean).join(' ')}
                          className={inputClass}
                        />
                      </div>
                      <div className="flex flex-col gap-1.5">
                        <label htmlFor={time} className="text-sm text-ink-soft">
                          Starts at
                        </label>
                        <input
                          id={time}
                          type="time"
                          value={slot.time}
                          onChange={(e) => setDate(slot.id, 'time', e.target.value)}
                          aria-invalid={error ? true : undefined}
                          aria-describedby={error ? `${day}-error` : undefined}
                          className={inputClass}
                        />
                      </div>
                      {form.dates.length > 1 && (
                        <Button variant="quiet" size="sm" onClick={() => removeDate(slot)} className="col-span-2 justify-self-start sm:col-span-1">
                          Remove<span className="sr-only"> day {i + 1}</span>
                        </Button>
                      )}
                    </div>
                    {error && (
                      <p id={`${day}-error`} className="text-sm font-medium text-no">
                        {error}
                      </p>
                    )}
                  </fieldset>
                </li>
              )
            })}
          </ol>
          {form.dates.length < MAX_DATES && (
            <div>
              <Button variant="secondary" size="sm" onClick={addDate}>
                <Icon name="plus" size={16} />
                Add another date
              </Button>
            </div>
          )}
        </div>
      </Section>

      <Tile className="sm:p-6">
        <details className="group" open={errors.window ? true : undefined}>
          <summary className="flex min-h-11 cursor-pointer list-none items-center justify-between gap-3 font-display text-xl font-bold">
            More options
            <Icon name="arrowRight" className="text-brand transition-transform duration-200 group-open:rotate-90" />
          </summary>
          <div className="mt-4">
            <Field id="f-window" label="Time to reserve" hint="How long people have to reserve once you open a date. At most 3 days, the time PayPal guarantees a hold." error={errors.window}>
              <select {...input('window', 'How long people have to reserve once you open a date. At most 3 days, the time PayPal guarantees a hold.')} className={`${inputClass} sm:max-w-xs`}>
                {WINDOWS.map((m) => (
                  <option key={m} value={m}>
                    {windowLabel(m)}
                  </option>
                ))}
              </select>
            </Field>
          </div>
        </details>
      </Tile>

      <div className="flex flex-col items-start gap-4">
        {errors.form && (
          <div id="f-form-error" tabIndex={-1}>
            <Notice tone="attention" role="alert">
              {errors.form}
            </Notice>
          </div>
        )}
        {Object.keys(errors).some((k) => k !== 'form') && (
          <p role="alert" className="font-medium text-no">
            Some fields need a fix before the workshop can be created.
          </p>
        )}
        <Button type="submit" size="lg" busy={busy} busyLabel="Creating…">
          Create workshop
        </Button>
        <p className="text-sm text-ink-soft">Nothing is charged until you open reservations on a date and enough people reserve.</p>
      </div>
    </form>
  )
}
