export function writeSession(key: string, value: string): boolean {
  const storage = globalThis.sessionStorage
  if (!storage) return false
  try {
    storage.setItem(key, value)
    return true
  } catch {
    return false
  }
}

export function readSession(key: string): string | null {
  try {
    return globalThis.sessionStorage?.getItem(key) ?? null
  } catch {
    return null
  }
}
