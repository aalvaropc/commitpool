import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router'
import { api } from '../api'
import { rememberDemo } from '../lib/demo'
import { Button, Icon, Notice, type ButtonSize } from './ui'

export function useDemoAvailable(): boolean {
  const [available, setAvailable] = useState(false)
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

export default function TryDemo({ size = 'lg' }: { size?: ButtonSize }) {
  const navigate = useNavigate()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function start() {
    setBusy(true)
    setError(null)
    try {
      const demo = await api.createDemo()
      rememberDemo(demo.owner_url, demo.participants)
      navigate(new URL(demo.owner_url).pathname)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'The demo could not start right now.')
      setBusy(false)
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <Button variant="go" size={size} onClick={start} busy={busy} busyLabel="Setting up Maya’s class…">
        Try it as Maya
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
