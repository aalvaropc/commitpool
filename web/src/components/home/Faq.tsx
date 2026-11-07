const faq = [
  {
    q: 'What is a hold?',
    a: 'Reserving places a temporary hold for the price on your PayPal account. Nothing is charged then. Holds are charged only once enough people have reserved for the class to happen.',
  },
  {
    q: 'What if not enough people reserve?',
    a: 'At the deadline the class is cancelled: every hold is released and any charge already made is refunded. Nobody pays for a class that does not happen.',
  },
  {
    q: 'Can I leave after reserving?',
    a: 'Yes, until you are charged: give up your spot and PayPal releases your hold. After that, payments are refunded only if the class is cancelled, so the group the organizer counted on stays whole.',
  },
  {
    q: 'Does anyone need an account?',
    a: 'Not a CommitPool account. The organizer gets a private link to their workshop; participants join from the invite link with a name and get a private link of their own. Reserving a spot happens at PayPal.',
  },
  {
    q: 'Is this real money?',
    a: 'Not in this demo. Everything runs on the PayPal Sandbox, PayPal’s test environment, and the demo class and its participants are prepared data.',
  },
]

function Sign() {
  return (
    <span aria-hidden="true" className="relative size-4 shrink-0 text-brand">
      <span className="absolute inset-x-0 top-1/2 h-[3px] -translate-y-1/2 rounded-full bg-current" />
      <span className="absolute inset-y-0 left-1/2 w-[3px] -translate-x-1/2 rounded-full bg-current transition-transform duration-200 group-open:rotate-90" />
    </span>
  )
}

export default function Faq() {
  return (
    <section aria-labelledby="faq-title" className="grid grid-cols-[minmax(0,1fr)] gap-8 lg:grid-cols-12">
      <h2 id="faq-title" className="shout text-head lg:sticky lg:top-8 lg:col-span-4 lg:self-start">
        Questions people ask
      </h2>
      <div className="flex flex-col gap-2 lg:col-span-8">
        {faq.map((f) => (
          <details key={f.q} name="faq" className="group rounded-3xl bg-surface transition-colors duration-200 open:bg-surface-muted">
            <summary className="flex min-h-[4.5rem] cursor-pointer list-none items-center justify-between gap-4 px-6 py-4 text-lg font-semibold [&::-webkit-details-marker]:hidden">
              {f.q}
              <Sign />
            </summary>
            <p className="max-w-[52ch] px-6 pb-6 text-lg text-ink-soft">{f.a}</p>
          </details>
        ))}
      </div>
    </section>
  )
}
