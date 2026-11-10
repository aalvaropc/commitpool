import type { CSSProperties } from 'react'

type Stroke = { d: string; w: number; o?: number; tone?: 'ember' | 'graphite' }

const charcoal = 'var(--color-charcoal)'
const graphite = 'var(--color-graphite-soft)'
const ember = 'var(--color-brand)'

const figure: Stroke[] = [
  { d: 'M18 60v570', w: 1.1, tone: 'graphite' },
  { d: 'M12 62h12M12 132h12M12 202h12M12 272h12M12 342h12M12 412h12M12 482h12M12 552h12M12 622h12', w: 1.1, tone: 'graphite' },
  { d: 'M160 62c-20-2-32 16-30 36 2 20 16 31 32 30 18-2 26-20 22-40-3-16-12-26-24-26', w: 2.5 },
  { d: 'M134 80c10-18 34-24 50-8', w: 1.2, o: 0.5 },
  { d: 'M138 72c14-12 30-12 42-2M141 66c12-8 26-8 36 0', w: 1, o: 0.4 },
  { d: 'M147 127c0 11-3 20-8 29M170 126c1 12 5 22 13 31', w: 2.2 },
  { d: 'M100 180c26-18 66-24 98-10', w: 2.6 },
  { d: 'M200 174c22-14 34-34 32-62', w: 2.5 },
  { d: 'M188 188c18-18 30-40 32-66', w: 1.6, o: 0.7 },
  { d: 'M232 112c-6-20-26-32-50-30', w: 2.4 },
  { d: 'M220 122c-8-14-22-22-38-22', w: 1.4, o: 0.6 },
  { d: 'M184 82c-6 2-8 8-6 14', w: 1.8 },
  { d: 'M108 190c-8 26-2 48 10 70-4 20-12 40-12 62', w: 2.5 },
  { d: 'M192 192c2 24-6 46-16 66 8 22 14 42 14 64', w: 2.5 },
  { d: 'M122 214c18 12 42 12 60-6', w: 1.2, o: 0.5 },
  { d: 'M104 182c-16 28-20 56-12 80 10 32 36 66 70 88', w: 2.4 },
  { d: 'M116 222c-6 18-8 34-4 48 8 26 28 52 54 72', w: 1.2, o: 0.5 },
  { d: 'M162 350c8 4 16 4 22 0', w: 2 },
  { d: 'M106 322c22 22 64 24 86 2', w: 2.3 },
  { d: 'M150 330c36 4 66 18 90 42', w: 2.6 },
  { d: 'M160 352c28 8 48 20 62 36', w: 2 },
  { d: 'M240 372c6 38-4 78-14 114', w: 2.5 },
  { d: 'M222 388c-10 32-16 66-16 98', w: 2 },
  { d: 'M206 488c10 10 30 12 44 4', w: 2.4 },
  { d: 'M108 332c-4 28 0 52 8 72', w: 2.5 },
  { d: 'M138 346c2 22 2 42-2 60', w: 2 },
  { d: 'M114 406c-8 64-10 132-6 192', w: 2.4 },
  { d: 'M138 408c-2 62-8 128-12 190', w: 2 },
  { d: 'M104 600c10 10 28 12 42 4', w: 2.4 },
  { d: 'M94 342c26 16 88 16 116-2', w: 2.2 },
  { d: 'M94 342c24-12 90-12 116-2', w: 1.1, o: 0.45 },
  { d: 'M110 352L84 626M198 350L226 626', w: 1.9, o: 0.8 },
  { d: 'M156 356L158 630', w: 1.4, o: 0.55 },
  { d: 'M90 520c40 10 96 10 132-2', w: 1.6, o: 0.8 },
  { d: 'M92 548c40 8 92 8 128-2', w: 1, o: 0.4 },
  { d: 'M178 226l12-12M176 240l14-14M174 254l14-14M176 268l12-12', w: 1.1, o: 0.55 },
  { d: 'M170 360l12-12M180 364l12-12M190 368l12-12M200 374l10-10', w: 1.1, o: 0.55 },
  { d: 'M122 424l12-10M120 444l14-12M120 464l14-12M118 484l14-12', w: 1.1, o: 0.55 },
  { d: 'M24 630c70-6 160 4 244-4', w: 1.6, o: 0.7 },
  { d: 'M44 637c60-2 120 2 190-2', w: 1, o: 0.4 },
  { d: 'M156 60c-18 64 18 120-4 186-10 30-6 60 2 90', w: 3.4, tone: 'ember' },
]

function stroke(tone: Stroke['tone']): string {
  return tone === 'ember' ? ember : tone === 'graphite' ? graphite : charcoal
}

export function SeatedFigure({ className = '' }: { className?: string }) {
  return (
    <svg viewBox="0 0 280 640" role="img" aria-label="A charcoal gesture drawing of a model seated on a tall stool, one arm raised behind her head" className={className}>
      <g fill="none" strokeLinecap="round" strokeLinejoin="round">
        {figure.map((s, i) => (
          <path
            key={s.d}
            d={s.d}
            pathLength={1}
            stroke={stroke(s.tone)}
            strokeWidth={s.w}
            opacity={s.o}
            className="sketch-line"
            style={{ '--i': s.tone === 'ember' ? figure.length + 4 : i } as CSSProperties}
          />
        ))}
      </g>
    </svg>
  )
}

export function Underline({ className = '' }: { className?: string }) {
  return (
    <svg viewBox="0 0 210 10" aria-hidden="true" focusable="false" className={className}>
      <path d="M2 6c40-4 92-5 150-2 20 1 38 2 56 0" fill="none" stroke={ember} strokeWidth="3" strokeLinecap="round" pathLength={1} className="sketch-line" style={{ '--i': 6 } as CSSProperties} />
    </svg>
  )
}

export function HandArrow({ className = '' }: { className?: string }) {
  return (
    <svg viewBox="0 0 96 52" aria-hidden="true" focusable="false" className={className}>
      <path d="M4 40c18-24 46-34 82-30" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" />
      <path d="M74 2l13 8-11 11" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

const easelX = [0, 92, 184, 276, 368, 460]

const canvasMarks = ['M40 40c-6 8-4 16 2 22s2 14-4 18', 'M126 44c8 2 14 10 10 20-2 6 2 12 8 14', 'M216 50c10-8 22-2 20 10s-14 14-8 22']

export function Easels({ reserved, open, className = '' }: { reserved: string[]; open: number; className?: string }) {
  const label = `${reserved.length + open} easels; ${reserved.length} hold a canvas marked in ember for ${reserved.join(', ').replace(/, ([^,]*)$/, ' and $1')}, ${open} are empty`
  const empty = easelX.slice(reserved.length, reserved.length + open)
  return (
    <div role="img" aria-label={label} className={className}>
      <svg viewBox="-6 0 552 126" aria-hidden="true" focusable="false" className="block h-auto w-full">
        <g fill="none" stroke={charcoal} strokeLinecap="round" strokeLinejoin="round">
          {easelX.map((x) => (
            <g key={x} transform={`translate(${x} 0)`}>
              <path d="M40 6L14 120M40 6L66 120" strokeWidth="2.2" />
              <path d="M40 4V124" strokeWidth="1.4" opacity="0.6" />
              <path d="M8 86h64" strokeWidth="2.4" />
              <path d="M10 89h60" strokeWidth="1" opacity="0.4" />
              <path d="M24 108h32" strokeWidth="1.4" />
            </g>
          ))}
        </g>
        {reserved.map((name, i) => (
          <g key={name} className="easel-canvas" style={{ '--i': i } as CSSProperties}>
            <rect x={easelX[i] + 17} y="30" width="46" height="56" rx="2" fill={ember} />
            <path d={canvasMarks[i % canvasMarks.length]} transform={`translate(${easelX[i] - easelX[i % canvasMarks.length]} 0)`} stroke="var(--color-on-brand)" strokeWidth="1.8" fill="none" strokeLinecap="round" />
          </g>
        ))}
        <g fill="none" stroke={graphite} strokeWidth="1.6" strokeDasharray="4 5">
          {empty.map((x) => (
            <rect key={x} x={x + 17} y="30" width="46" height="56" rx="2" />
          ))}
        </g>
      </svg>
      <div aria-hidden="true" className="mt-1.5 grid grid-cols-6 text-center text-[0.8125rem] leading-tight">
        {reserved.map((name) => (
          <span key={name} className="font-semibold text-charcoal">
            {name}
          </span>
        ))}
        {empty.map((x) => (
          <span key={x} className="font-medium text-graphite">
            open
          </span>
        ))}
      </div>
    </div>
  )
}

export function CharcoalStick({ className = '' }: { className?: string }) {
  return (
    <svg viewBox="0 0 300 170" role="img" aria-label="A charcoal stick drawing a gesture line" className={className}>
      <g fill="none" stroke={charcoal} strokeLinecap="round" strokeLinejoin="round">
        <path d="M18 140c30-40 62-58 96-50 30 8 34 40 64 42 22 2 38-12 52-30" stroke={ember} strokeWidth="3.2" />
        <path d="M22 146c34-36 66-52 96-44" strokeWidth="1" opacity="0.4" />
        <path d="M226 104l44-78" strokeWidth="2.4" />
        <path d="M238 110l44-78" strokeWidth="2.4" />
        <path d="M226 104l12 6M270 26l12 6" strokeWidth="2.2" />
        <path d="M229 99l11 6M232 93l11 6M235 88l11 6" strokeWidth="1.1" opacity="0.6" />
        <path d="M240 112c-6 10-12 14-18 14" strokeWidth="1.8" />
      </g>
    </svg>
  )
}
