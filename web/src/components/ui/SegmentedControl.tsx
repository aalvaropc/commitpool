import { useId } from 'react'

export interface Segment<T extends string> {
  value: T
  label: string
}

interface SegmentedControlProps<T extends string> {
  legend: string
  legendHidden?: boolean
  name?: string
  value: T
  options: Segment<T>[]
  disabled?: boolean
  onChange: (value: T) => void
}

export function SegmentedControl<T extends string>({ legend, legendHidden = true, name, value, options, disabled = false, onChange }: SegmentedControlProps<T>) {
  const generated = useId()
  const group = name ?? generated
  return (
    <fieldset disabled={disabled} className="min-w-0">
      <legend className={legendHidden ? 'sr-only' : 'mb-1.5 text-sm font-semibold'}>{legend}</legend>
      <div className="flex flex-wrap gap-1 rounded-control bg-surface-muted p-1 ring-1 ring-line ring-inset">
        {options.map((o) => {
          const id = `${group}-${o.value}`
          return (
            <span key={o.value} className="flex-1">
              <input id={id} type="radio" name={group} value={o.value} checked={value === o.value} onChange={() => onChange(o.value)} className="peer sr-only" />
              <label
                htmlFor={id}
                className="flex min-h-11 cursor-pointer items-center justify-center rounded-[0.625rem] px-2 text-sm font-semibold whitespace-nowrap text-ink-soft transition-colors duration-150 peer-checked:bg-brand peer-checked:text-white peer-checked:shadow-tile peer-focus-visible:outline-3 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-brand peer-disabled:cursor-not-allowed peer-disabled:opacity-60 hover:text-ink"
              >
                {o.label}
              </label>
            </span>
          )
        })}
      </div>
    </fieldset>
  )
}
