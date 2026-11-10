import type { SVGProps } from 'react'

const paths = {
  check: 'M5 12.5l4.2 4.2L19 7',
  clock: 'M12 7v5l3 2M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z',
  x: 'M6.5 6.5l11 11M17.5 6.5l-11 11',
  help: 'M9.5 9.25a2.6 2.6 0 0 1 5 .95c0 1.75-2.5 2.3-2.5 3.8M12 17.25h.01M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z',
  calendar: 'M7 3v3M17 3v3M3.5 9.5h17M5 5h14a1.5 1.5 0 0 1 1.5 1.5v12A1.5 1.5 0 0 1 19 20H5a1.5 1.5 0 0 1-1.5-1.5v-12A1.5 1.5 0 0 1 5 5Z',
  pin: 'M12 21s-6.5-5.6-6.5-11a6.5 6.5 0 0 1 13 0c0 5.4-6.5 11-6.5 11ZM12 12.25a2.25 2.25 0 1 0 0-4.5 2.25 2.25 0 0 0 0 4.5Z',
  users: 'M16 19.5v-1.25A3.75 3.75 0 0 0 12.25 14.5h-4.5A3.75 3.75 0 0 0 4 18.25v1.25M10 11a3.25 3.25 0 1 0 0-6.5 3.25 3.25 0 0 0 0 6.5ZM20 19.5v-1.25a3.75 3.75 0 0 0-2.75-3.6M15.5 4.6a3.25 3.25 0 0 1 0 6.3',
  lock: 'M7.5 10.5V8a4.5 4.5 0 0 1 9 0v2.5M6 10.5h12a1 1 0 0 1 1 1v8a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1v-8a1 1 0 0 1 1-1Z',
  arrowRight: 'M5 12h14M13 6l6 6-6 6',
  plus: 'M12 5v14M5 12h14',
  copy: 'M9 9h10.5v10.5H9zM15 9V4.5H4.5V15H9',
  spark: 'M12 3l2.1 6L20 11l-5.9 2L12 19l-2.1-6L4 11l5.9-2Z',
  send: 'M5 12l14-7-5 14-2.5-5.5ZM11.5 13.5L19 5',
  quote: 'M7 17.5c-1.9 0-3-1.4-3-3.4C4 10.8 6.3 8 9.2 6.5M17 17.5c-1.9 0-3-1.4-3-3.4 0-3.3 2.3-6.1 5.2-7.6',
  out: 'M14 5h5v5M19 5l-8 8M17 14v4.5a1.5 1.5 0 0 1-1.5 1.5h-10A1.5 1.5 0 0 1 4 18.5v-10A1.5 1.5 0 0 1 5.5 7H10',
} as const

export type IconName = keyof typeof paths

interface IconProps extends Omit<SVGProps<SVGSVGElement>, 'name'> {
  name: IconName
  size?: number
}

export function Icon({ name, size = 18, className = '', ...rest }: IconProps) {
  return (
    <svg
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      className={`shrink-0 ${className}`}
      {...rest}
    >
      <path d={paths[name]} />
    </svg>
  )
}
