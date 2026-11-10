import { useState } from 'react'
import { useNavigate } from 'react-router'
import { api } from '../api'
import { useDemoInfo } from '../lib/demoInfo'
import { Button, Icon, Notice, type ButtonSize, type ButtonVariant } from './ui'

export function useDemoAvailable(): boolean | undefined {
  const info = useDemoInfo()
  return info === undefined ? undefined : info?.workshops === true
}

interface TryDemoProps {
  size?: ButtonSize
  variant?: ButtonVariant
  pending?: boolean
  className?: string
  floatingError?: boolean
}

export default function TryDemo({ size = 'lg', variant = 'primary', pending = false, className = '', floatingError = false }: TryDemoProps) {
  const navigate = useNavigate()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function start() {
    setBusy(true)
    setError(null)
    try {
      const demo = await api.createDemo()
      navigate(new URL(demo.owner_url).pathname)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'The demo could not start right now.')
      setBusy(false)
    }
  }

  if (pending) {
    return (
      <div aria-hidden="true" className="invisible">
        <Button size={size} variant={variant} tabIndex={-1}>
          Try it as Iris
          <Icon name="arrowRight" size={20} />
        </Button>
      </div>
    )
  }

  return (
    <div className={`flex flex-col gap-2 ${floatingError ? 'relative' : ''}`}>
      <Button size={size} variant={variant} onClick={start} busy={busy} busyLabel="Setting up Iris’s class…" className={className}>
        Try it as Iris
        <Icon name="arrowRight" size={size === 'sm' ? 16 : 20} />
      </Button>
      {error && (
        <Notice tone="attention" role="alert" className={floatingError ? 'absolute top-full right-0 z-20 mt-2 w-72' : undefined}>
          {error}
        </Notice>
      )}
    </div>
  )
}
