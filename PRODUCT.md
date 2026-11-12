# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

- **Organizers**: independent instructors who run small paid group classes (life drawing, photography, cooking, yoga and the like). Before they commit to a date and pay for a room, a model or materials, they need to know enough people will actually pay to cover it. Today they chase a group chat full of "maybe" and absorb the no-shows.
- **Participants**: people invited through a shared link, on their phone or laptop, without an account. They say when they can come in their own words, and reserve a spot with PayPal knowing they are charged only if the class happens.

## Product Purpose

CommitPool turns "I'm in" into a commitment that only costs money when the class is real. The organizer proposes dates and pastes the group chat; Claude reads each person's words against the dates; CommitPool picks the date that reaches the minimum, or says which tied date comes first; participants reserve with a PayPal hold; the holds are charged only once enough people have reserved, and everyone is released or refunded if the group falls short. Success is a class that either happens with its room paid for, or is cancelled cleanly with nobody out of pocket.

## Positioning

"Doodle meets Kickstarter, for small paid classes." The headline: **find the date that fills the class.** The promise behind it: the class happens, or nobody pays. What a neighbouring product cannot claim: the date comes from what people write (Claude reads each message into a verdict per date; the server, not the model, decides who can come and which date to recommend), and the money moves only when the minimum is met, through PayPal's authorize-then-capture.

## Operating Context

- An organizer creates a class, pastes the group chat (or shares the invite link in it), sees Claude read each person against the dates, opens reservations on the date CommitPool picks, and sees the class confirm or cancel.
- A participant opens the link, joins with a name (email optional), writes when they can come, corrects the reading if needed, reads the terms and reserves at PayPal.
- An organizer can also run the class from an AI assistant through CommitPool's MCP server; no tool moves money, a human approves every hold at PayPal.
- Payments run on the PayPal Sandbox in the hosted demo; nothing real is charged.

## Capabilities and Constraints

- Classes: title, description, organizer's name, place, duration, price (USD), people needed, spots, optional private session cost (room, model, materials) (suggests the break-even minimum), one to ten candidate dates, time to reserve up to 72 hours.
- Availability in plain words, read by Claude on Amazon Bedrock; the participant can correct each verdict; a message that leaves the class hours open gets one question from Claude instead of a guess.
- Payments: one PayPal order per spot, a hold, a charge once enough people reserve, a release or refund otherwise; organizer cancel; participant can give up their spot until charging starts; operator actions for anything PayPal leaves unsettled.
- Group chat import (pasted text or WhatsApp's `.txt`; pasting again reads only new messages), Claude's reply for the group, a payout to the organizer at the class start less PayPal's fees, and one deadline extension for a class short of its minimum.
- Terminology in the product: reserve, spot, hold (in the terms), it's on, cancelled, refunded, released. Not "authorize", "capture", "seat", "capacity" or "minimum paid seats" in what people read.
- No accounts: private links are the access. The code carries no comments.

## Brand Commitments

- Name: CommitPool. Headline: "Find the date that fills the class." Promise line, kept as the secondary line: "The class happens, or nobody pays."
- Voice: second person, short sentences, concrete numbers, no payment jargon outside the terms.
- Country-agnostic: no country, city or tax in product copy beyond what an organizer types.
- Demo persona: Iris Calder, a fictional life drawing teacher running "Life drawing for beginners" at "The Long Room · Red Hook, Brooklyn" (room and model $120 a session, $45 a person, 3 needed, 6 spots). Always labelled as prepared demo data, never presented as a real customer.

## Evidence on Hand

- No testimonials, customers, usage numbers or press. None may be invented.
- Public data only, already verified: PheedLoop event data (small free events about 37% median no-show, small paid events about 17%); O*NET/BLS 2025 employment for self-enrichment teachers (419,100) and fitness instructors (388,400).
- Real working demo on PayPal Sandbox with Claude on Amazon Bedrock.

## Product Principles

1. Money never gets ahead of the facts: no screen says charged, released or refunded before PayPal says so.
2. The person decides; the software prepares. Claude reads and explains, the server computes, a human approves every hold.
3. Show the group, not the plumbing: who can come, how close the class is to happening, what it costs.
4. Plain words over payment terms, everywhere a person reads.
5. Nothing fake presented as real: demo data is labelled, claims are verifiable.

## Accessibility & Inclusion

WCAG 2.2 AA: text contrast, visible focus, keyboard reach for every action, native form controls, status changes announced, reduced motion respected, layouts that work at phone width. English only.
