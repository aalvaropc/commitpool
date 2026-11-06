import type { ReactNode } from 'react'

export function Page({ children, wide = false }: { children: ReactNode; wide?: boolean }) {
  return <div className={`mx-auto w-full ${wide ? 'max-w-6xl' : 'max-w-3xl'} px-4 py-6 sm:px-6 sm:py-10`}>{children}</div>
}
