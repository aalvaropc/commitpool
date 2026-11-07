import { useEffect, useRef, useState, type RefObject } from 'react'

export function useInView<T extends Element>(): [RefObject<T | null>, boolean] {
  const ref = useRef<T>(null)
  const [seen, setSeen] = useState(() => typeof IntersectionObserver === 'undefined')
  useEffect(() => {
    const node = ref.current
    if (!node || seen) return
    const watcher = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          setSeen(true)
          watcher.disconnect()
        }
      },
      { threshold: 0, rootMargin: '0px 0px -15% 0px' },
    )
    watcher.observe(node)
    return () => watcher.disconnect()
  }, [seen])
  return [ref, seen]
}
