import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { startPolling, type VisibilitySource } from './polling'

class FakeDocument extends EventTarget implements VisibilitySource {
  visibilityState: DocumentVisibilityState = 'visible'
  show() {
    this.visibilityState = 'visible'
    this.dispatchEvent(new Event('visibilitychange'))
  }
  hide() {
    this.visibilityState = 'hidden'
    this.dispatchEvent(new Event('visibilitychange'))
  }
}

describe('startPolling', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('refreshes on every interval while visible', async () => {
    const fn = vi.fn(async () => {})
    const stop = startPolling(fn, 1000, new FakeDocument())
    await vi.advanceTimersByTimeAsync(3000)
    expect(fn).toHaveBeenCalledTimes(3)
    stop()
  })

  it('skips hidden tabs and refreshes as soon as the tab is back', async () => {
    const doc = new FakeDocument()
    const fn = vi.fn(async () => {})
    const stop = startPolling(fn, 1000, doc)
    doc.hide()
    await vi.advanceTimersByTimeAsync(5000)
    expect(fn).not.toHaveBeenCalled()
    doc.show()
    await vi.advanceTimersByTimeAsync(0)
    expect(fn).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1000)
    expect(fn).toHaveBeenCalledTimes(2)
    stop()
  })

  it('never runs two refresh loops at once', async () => {
    const doc = new FakeDocument()
    let finish: () => void = () => {}
    const fn = vi.fn(() => new Promise<void>((r) => (finish = r)))
    const stop = startPolling(fn, 1000, doc)
    await vi.advanceTimersByTimeAsync(1000)
    expect(fn).toHaveBeenCalledTimes(1)
    doc.show()
    await vi.advanceTimersByTimeAsync(0)
    expect(fn).toHaveBeenCalledTimes(1)
    finish()
    await vi.advanceTimersByTimeAsync(1000)
    expect(fn).toHaveBeenCalledTimes(2)
    finish()
    await vi.advanceTimersByTimeAsync(1000)
    expect(fn).toHaveBeenCalledTimes(3)
    stop()
  })

  it('stops for good', async () => {
    const doc = new FakeDocument()
    const fn = vi.fn(async () => {})
    startPolling(fn, 1000, doc)()
    doc.show()
    await vi.advanceTimersByTimeAsync(5000)
    expect(fn).not.toHaveBeenCalled()
  })

  it('keeps polling after a failed refresh', async () => {
    const fn = vi.fn(async () => {
      throw new Error('offline')
    })
    const stop = startPolling(fn, 1000, new FakeDocument())
    await vi.advanceTimersByTimeAsync(2000)
    expect(fn).toHaveBeenCalledTimes(2)
    stop()
  })
})
