import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router'
import { api } from '../api'
import { Button, Icon, Notice, type ButtonSize, type ButtonVariant } from './ui'

export function useDemoAvailable(): boolean | undefined {
  const [available, setAvailable] = useState<boolean>()
  useEffect(() => {
    let live = true
    api
      .demoInfo()
      .then((info) => live && setAvailable(info.workshops))
      .catch(() => live && setAvailable(false))
    return () => {
      live = false
    }
  }, [])
  return available
}

export default function TryDemo({ size = 'lg', variant = 'primary', pending = false }: { size?: ButtonSize; variant?: ButtonVariant; pending?: boolean }) {
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
    <div className="flex flex-col gap-2">
      <Button size={size} variant={variant} onClick={start} busy={busy} busyLabel="Setting up Iris’s class…">
        Try it as Iris
        <Icon name="arrowRight" size={20} />
      </Button>
      {error && (
        <Notice tone="attention" role="alert">
          {error}
        </Notice>
      )}
    </div>
  )
}
