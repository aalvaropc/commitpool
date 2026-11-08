import { useInView } from '../../lib/inView'

const turns = [
  { who: 'you', text: 'How is my class going?' },
  { who: 'tool', text: 'get_status' },
  { who: 'claude', text: 'Saturday has 2 of the 3 you need. Grace might swap a market morning but didn’t say which, so I drafted a question: the class runs 10 AM–12 PM, can she make either morning?' },
  { who: 'you', text: 'She answered. How about now?' },
  { who: 'tool', text: 'get_status' },
  { who: 'claude', text: 'Grace swapped Saturday, so it works for 3 — you need 3. Reservations can open.' },
  { who: 'you', text: 'Open reservations on Saturday.' },
  { who: 'tool', text: 'preview_open_payments' },
  { who: 'claude', text: 'Here are the exact terms: $45 held per person, charged only if 3 reserve. Open them?' },
  { who: 'you', text: 'yes' },
  { who: 'tool', text: 'confirm_open_payments' },
  { who: 'claude', text: 'Reservations are open. Each person approves their own hold at PayPal.' },
] as const

export default function McpTranscript() {
  const [ref, seen] = useInView<HTMLOListElement>()
  return (
    <ol ref={ref} aria-label="Example conversation with an AI assistant" className="flex flex-col gap-3 rounded-[2rem] bg-surface p-5 sm:p-8">
      {turns.map((t, i) => (
        <li key={i} className={`flex ${t.who === 'you' ? 'justify-end' : ''} ${seen ? 'rise' : 'opacity-0'}`} style={seen ? { animationDelay: `${i * 0.35}s` } : undefined}>
          {t.who === 'tool' ? (
            <span className="rounded-full bg-hold-wash px-3.5 py-1.5 font-mono text-sm text-hold">
              <span className="sr-only">Tool call: </span>
              {t.text}
            </span>
          ) : t.who === 'you' ? (
            <p className="max-w-[80%] rounded-3xl rounded-br-md bg-brand px-4 py-2.5 font-medium text-on-brand">{t.text}</p>
          ) : (
            <p className="max-w-[88%] rounded-3xl rounded-bl-md bg-surface-muted px-4 py-2.5">{t.text}</p>
          )}
        </li>
      ))}
    </ol>
  )
}
