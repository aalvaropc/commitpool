import { Icon } from '../ui'
import { CharcoalStick } from './Drawings'
import { PaperSection } from './Section'

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
    q: 'Does Claude decide the date?',
    a: 'No. Claude reads each answer and asks when something is unclear. CommitPool counts who can come and recommends the date; the organizer opens reservations.',
  },
  {
    q: 'Can I leave after reserving?',
    a: 'Yes, until charging starts: give up your spot and PayPal releases your hold. After that, payments are refunded only if the class is cancelled, so the group the organizer counted on stays whole.',
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

export default function Faq() {
  return (
    <PaperSection id="faq" labelledBy="faq-title">
      <div className="wrap section-pad grid gap-8 lg:grid-cols-12 lg:gap-x-8">
        <div className="grid content-start gap-[1.375rem] lg:col-span-4">
          <h2 id="faq-title" className="shout text-section">
            Questions people ask
          </h2>
          <CharcoalStick className="h-auto w-full max-w-60" />
        </div>
        <div className="grid border-t-2 border-charcoal lg:col-span-7 lg:col-start-6">
          {faq.map((f) => (
            <details key={f.q} name="faq" className="group border-b border-paper-rule">
              <summary className="flex min-h-[4.25rem] cursor-pointer list-none items-center justify-between gap-4 py-4 text-[1.1875rem] font-bold [&::-webkit-details-marker]:hidden">
                {f.q}
                <Icon name="plus" size={20} strokeWidth={2.4} className="text-ember-ink transition-transform duration-200 ease-(--ease-out) group-open:rotate-45" />
              </summary>
              <p className="max-w-[56ch] pb-[1.375rem] text-graphite">{f.a}</p>
            </details>
          ))}
        </div>
      </div>
    </PaperSection>
  )
}
