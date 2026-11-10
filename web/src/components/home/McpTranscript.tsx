import type { ReactNode } from 'react'
import { Avatar, Icon } from '../ui'
import { PaperSection, SectionHead } from './Section'

function You({ children }: { children: ReactNode }) {
  return (
    <li className="max-w-[80%] justify-self-end rounded-[1.25rem] rounded-br-[0.35rem] bg-surface-muted px-3.5 py-2.5 text-[0.9375rem]">
      <span className="sr-only">Iris: </span>
      {children}
    </li>
  )
}

function Tool({ name, detail }: { name: string; detail: string }) {
  return (
    <li className="flex flex-wrap items-center gap-x-2.5 gap-y-1 font-mono text-[0.8125rem] text-ink-faint">
      <Icon name="spark" size={14} className="text-hold" />
      <span className="sr-only">Tool call: </span>
      <b className="font-semibold text-hold">{name}</b>
      <span aria-hidden="true">·</span>
      {detail}
    </li>
  )
}

function Assistant({ children }: { children: ReactNode }) {
  return (
    <li className="max-w-[56ch] text-[0.9375rem] leading-normal">
      <span className="sr-only">Assistant: </span>
      {children}
    </li>
  )
}

const terms = ['Sat · 10 AM–12 PM', '$45', '3 needed', '6 spots', '24 h to reserve']

export default function McpSection({ origin }: { origin: string }) {
  return (
    <PaperSection labelledBy="mcp-title">
      <div className="wrap section-pad grid gap-10 lg:grid-cols-12 lg:gap-x-8">
        <div className="lg:col-span-5">
          <SectionHead id="mcp-title" title="Run it from your AI assistant." tone="paper" stacked className="">
            CommitPool is also an MCP server. Your assistant checks who can come and prepares reservations. It can’t move money: Iris approves on her own page.
          </SectionHead>
          <figure className="m-0 mt-[1.625rem]">
            <pre role="region" tabIndex={0} aria-labelledby="mcp-command" className="kiln overflow-x-auto rounded-control bg-charcoal px-4 py-3.5 font-mono text-[0.8125rem] leading-relaxed whitespace-pre-wrap [overflow-wrap:anywhere]">
              <code>
                <span aria-hidden="true" className="text-brand-deep">
                  ${' '}
                </span>
                claude mcp add --transport http commitpool {origin}/mcp
              </code>
            </pre>
            <figcaption id="mcp-command" className="mt-2.5 text-sm text-graphite">
              Add it to Claude Code. It works with Claude Desktop and any MCP client too.
            </figcaption>
          </figure>
        </div>
        <figure className="kiln m-0 self-start overflow-hidden rounded-tile bg-canvas lg:col-span-6 lg:col-start-7">
          <figcaption className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 bg-surface px-[1.125rem] py-3.5 text-[0.8125rem] text-ink-faint">
            <span>
              <b className="font-bold text-ink">Iris’s assistant</b> · CommitPool connected · example
            </span>
            <span>3 tools used</span>
          </figcaption>
          <ol aria-label="Iris’s assistant opens reservations" className="grid gap-3.5 p-[1.125rem]">
            <You>How’s my life drawing class going?</You>
            <Tool name="get_status" detail="Life drawing for beginners" />
            <Assistant>Saturday has 3 who can come — Priya, Ben and Grace — and you need 3. Hannah has to leave by 11:30. Open reservations for Saturday?</Assistant>
            <You>Yes, open Saturday.</You>
            <Tool name="preview_open_payments" detail="Saturday" />
            <Assistant>
              These terms lock once reservations open. Tap Approve on your page; I can’t approve it for you.
              <span className="mt-2 flex flex-wrap gap-1.5">
                {terms.map((t) => (
                  <span key={t} className="rounded-full bg-surface px-2.5 py-0.5 text-[0.8125rem] text-ink-soft">
                    {t}
                  </span>
                ))}
              </span>
            </Assistant>
            <li className="grid gap-3 rounded-[1.25rem] border border-brand/50 bg-brand-wash p-4">
              <p className="flex items-center gap-2.5 text-[0.8125rem] font-semibold text-brand">
                <Avatar name="Iris Calder" size="sm" />
                On Iris’s phone · her organizer page
              </p>
              <p className="text-[1.0625rem] leading-snug font-bold">Your assistant wants to open reservations on these terms.</p>
              <p className="flex flex-wrap items-center gap-2.5">
                <span aria-hidden="true" className="inline-flex min-h-11 items-center gap-2 rounded-full bg-brand px-5 text-[0.9375rem] font-bold text-on-brand">
                  <Icon name="check" size={16} />
                  Approve
                </span>
                <span className="inline-flex items-center gap-1.5 text-sm font-semibold text-paid">
                  <Icon name="check" size={15} />
                  Approved by Iris · 9:12 PM
                </span>
              </p>
            </li>
            <Tool name="confirm_open_payments" detail="approved" />
            <Assistant>Reservations are open for 24 hours. Nobody is charged until 3 people reserve.</Assistant>
          </ol>
        </figure>
      </div>
    </PaperSection>
  )
}
