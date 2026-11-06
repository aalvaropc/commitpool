import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError } from '../api'
import { startPolling } from './polling'

export interface Resource<T> {
  data?: T
  error?: ApiError
  reload: () => Promise<T | undefined>
}

function asApiError(e: unknown): ApiError {
  return e instanceof ApiError ? e : new ApiError(0, 'unexpected', 'Something went wrong. Please try again.')
}

export function useResource<T>(load: () => Promise<T>, key: string): Resource<T> {
  const [state, setState] = useState<{ data?: T; error?: ApiError }>({})
  const loadRef = useRef(load)
  loadRef.current = load
  const seq = useRef(0)

  const reload = useCallback(async () => {
    const mine = ++seq.current
    try {
      const data = await loadRef.current()
      if (mine === seq.current) setState({ data })
      return data
    } catch (e) {
      if (mine === seq.current) setState((s) => ({ data: s.data, error: asApiError(e) }))
      return undefined
    }
  }, [])

  useEffect(() => {
    setState({})
    void reload()
  }, [key, reload])

  return { ...state, reload }
}

export function usePolling(fn: () => Promise<unknown>, ms: number, active: boolean): void {
  const fnRef = useRef(fn)
  fnRef.current = fn
  useEffect(() => {
    if (!active) return
    return startPolling(() => fnRef.current(), ms, globalThis.document)
  }, [active, ms])
}

export function useNow(ms: number): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), ms)
    return () => clearInterval(id)
  }, [ms])
  return now
}
