import { useEffect, useState } from 'react'
import { api, type DemoInfo } from '../api'

let shared: Promise<DemoInfo> | null = null

function loadDemoInfo(): Promise<DemoInfo> {
  if (!shared) {
    shared = api.demoInfo()
    shared.catch(() => {
      shared = null
    })
  }
  return shared
}

export function useDemoInfo(): DemoInfo | null | undefined {
  const [info, setInfo] = useState<DemoInfo | null>()
  useEffect(() => {
    let live = true
    loadDemoInfo()
      .then((i) => live && setInfo(i))
      .catch(() => live && setInfo(null))
    return () => {
      live = false
    }
  }, [])
  return info
}

const EMBED_PREFIX = 'https://www.youtube-nocookie.com/embed/'

export function embeddableVideo(url: string | undefined): string | null {
  if (!url?.startsWith(EMBED_PREFIX)) return null
  return /^[A-Za-z0-9_-]{11}$/.test(url.slice(EMBED_PREFIX.length)) ? url : null
}
