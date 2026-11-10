import { Icon } from '../ui'
import { SectionHead } from './Section'

const apis = [
  { name: 'Orders v2', calls: 'create · authorize', does: 'Opens a PayPal checkout for each spot and holds the price once the buyer approves.' },
  { name: 'Payments v2', calls: 'capture · void · refund', does: 'Charges the holds at the minimum, releases them if the class falls short, refunds a charge if it’s cancelled later.' },
  { name: 'Payouts', calls: 'payouts batch', does: 'Pays the organizer when the class starts.' },
  { name: 'Webhooks', calls: 'verify-webhook-signature', does: 'Every event is verified with PayPal before it changes a spot.' },
]

const records = [
  { kind: 'Order', id: '8K886057PL996893M', status: 'COMPLETED' },
  { kind: 'Authorization', id: '1AS808255V146505P', status: 'CAPTURED' },
  { kind: 'Capture', id: '82975102TH652782Y', status: 'REFUNDED' },
  { kind: 'Refund', id: '43G324002P691632L', status: 'COMPLETED' },
]

export default function PayPalProof() {
  return (
    <section id="paypal" aria-labelledby="pp-title" className="section-pad scroll-mt-4">
      <div className="wrap">
        <SectionHead id="pp-title" title="PayPal, for real.">
          Every hold, charge, refund and payout is a PayPal Sandbox call, and each one can be checked against PayPal.
        </SectionHead>
        <div className="grid gap-12 lg:grid-cols-12 lg:gap-x-8">
          <dl className="grid self-start border-t border-line lg:col-span-6">
            {apis.map((a) => (
              <div key={a.name} className="grid gap-x-5 gap-y-1 border-b border-line py-[1.125rem] md:grid-cols-[9.375rem_1fr]">
                <dt className="text-lg font-bold">{a.name}</dt>
                <dd className="font-mono text-[0.8125rem] text-brand-deep md:col-start-1">{a.calls}</dd>
                <dd className="text-[0.9375rem] text-ink-soft md:col-start-2 md:row-span-2 md:row-start-1 md:self-center">{a.does}</dd>
              </div>
            ))}
          </dl>
          <figure className="m-0 grid content-start gap-[1.125rem] rounded-tile bg-surface p-[1.375rem] lg:col-span-5 lg:col-start-8">
            <figcaption>
              <strong className="block text-[1.0625rem]">A refunded reservation</strong>
              <span className="mt-0.5 block text-[0.8125rem] leading-snug text-ink-faint">What “Verify with PayPal” checks on Iris’s board, with IDs from a PayPal Sandbox run.</span>
            </figcaption>
            <p className="flex items-center gap-3.5 rounded-control bg-paid-wash px-[1.125rem] py-4 text-paid">
              <Icon name="check" size={30} strokeWidth={2.2} />
              <b className="shout text-[clamp(1.375rem,1rem+1.4vw,2rem)] leading-none tracking-[-0.01em] normal-case">4 of 4 records match PayPal</b>
            </p>
            <dl className="grid">
              {records.map((r) => (
                <div key={r.kind} className="grid grid-cols-[1fr_auto] items-center gap-x-3 gap-y-0.5 border-t border-line py-3">
                  <dt className="text-[0.8125rem] text-ink-faint">{r.kind}</dt>
                  <dd className="row-span-2 inline-flex items-center gap-1.5 text-[0.8125rem] font-semibold text-paid">
                    <Icon name="check" size={15} strokeWidth={2.2} />
                    Match
                  </dd>
                  <dd className="font-mono text-sm tracking-[0.02em] [overflow-wrap:anywhere]">
                    {r.id} <span className="text-[0.8125rem] text-ink-soft">{r.status}</span>
                  </dd>
                </div>
              ))}
            </dl>
          </figure>
        </div>
      </div>
    </section>
  )
}
