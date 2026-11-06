export interface VisibilitySource {
  visibilityState: DocumentVisibilityState
  addEventListener(type: 'visibilitychange', listener: () => void): void
  removeEventListener(type: 'visibilitychange', listener: () => void): void
}

export function startPolling(fn: () => Promise<unknown>, ms: number, doc?: VisibilitySource): () => void {
  let stopped = false
  let running = false
  let timer: ReturnType<typeof setTimeout> | undefined

  const schedule = () => {
    clearTimeout(timer)
    if (!stopped) timer = setTimeout(tick, ms)
  }

  async function tick() {
    if (running || stopped) return
    running = true
    if (doc?.visibilityState !== 'hidden') await fn().catch(() => undefined)
    running = false
    schedule()
  }

  const onVisibilityChange = () => {
    if (doc?.visibilityState !== 'visible' || running) return
    clearTimeout(timer)
    void tick()
  }

  doc?.addEventListener('visibilitychange', onVisibilityChange)
  schedule()
  return () => {
    stopped = true
    clearTimeout(timer)
    doc?.removeEventListener('visibilitychange', onVisibilityChange)
  }
}
