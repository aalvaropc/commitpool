export type WorkshopStatus = 'COLLECTING' | 'FUNDING' | 'CONFIRMED' | 'CANCELLED'
export type Resolution = 'OPEN' | 'RESOLVING' | 'SETTLED' | 'NEEDS_ATTENTION'
export type EnrollmentStatus =
  | 'SEAT_RESERVED'
  | 'AUTHORIZED'
  | 'CAPTURING'
  | 'CAPTURE_PENDING'
  | 'PAID'
  | 'CAPTURE_FAILED'
  | 'VOIDING'
  | 'VOIDED'
  | 'REFUNDING'
  | 'REFUND_PENDING'
  | 'REFUNDED'
  | 'REFUND_FAILED'
  | 'NEEDS_ATTENTION'
  | 'RELEASED'

export interface Money {
  minor: number
  currency: string
  value: string
}

export interface Workshop {
  id?: string
  title: string
  description: string
  duration_min: number
  timezone: string
  minimum: number
  capacity: number
  price: Money
  currency: string
  payment_window_min: number
  status: WorkshopStatus
  resolution: Resolution
  host_name: string
  location: string
  venue_cost?: Money
  break_even_seats?: number
}

export interface CandidateDate {
  id: string
  starts_at: string
  ends_at: string
  label: string
}

export interface Proposal {
  id: string
  version: number
  date_id: string
  price: Money
  minimum: number
  capacity: number
  opens_at: string
  closes_at: string
  starts_at: string
  payments_open: boolean
}

export interface Tally {
  interested: number
  authorized: number
  paid: number
  minimum: number
  capacity: number
}

export interface OwnerTally extends Tally {
  reserved: number
  capturing: number
  pending: number
  failed: number
  seats_taken: number
}

export interface Payment {
  attempt_id: string
  attempt_no: number
  order_id: string | null
  authorization_id: string | null
  capture_id: string | null
  refund_id: string | null
  order_status: string | null
  authorization_status: string | null
  capture_status: string | null
  capture_issue: string | null
  refund_status: string | null
}

export interface Enrollment {
  id: string
  participant_id?: string
  display_name?: string
  status: EnrollmentStatus
  status_label: string
  seat_expires_at: string
  approval_cancelled_at: string | null
  money_status?: EnrollmentStatus
  needs_attention?: boolean
  payment: Payment | null
}

export type Verdict = 'available' | 'unavailable' | 'conditional' | 'unknown'
export type ConditionType = 'none' | 'end_before' | 'start_after'

export interface Condition {
  type: ConditionType
  time: string
}

export interface DateVerdict {
  candidate_date_id: string
  verdict: Verdict
  condition: Condition
  evidence: string
}

export interface Ambiguity {
  question: string
  why: string
}

export interface Interpretation {
  per_date: DateVerdict[]
  hard_constraints: string[]
  preferences: string[]
  ambiguities: Ambiguity[]
  needs_clarification: boolean
}

export interface Availability {
  id: string
  participant_id?: string
  display_name?: string
  message: string
  interpretation: Interpretation
  corrected: Interpretation | null
  needs_clarification: boolean
  created_at: string
}

export interface DateSummary {
  date_id: string
  compatible: string[] | null
  blockers: Record<string, string> | null
}

export interface Recommendation {
  date_id: string
  meets_minimum: boolean
  per_date: DateSummary[] | null
  explanation: string
  explanation_pending: boolean
}

export interface ParticipantRef {
  id: string
  display_name: string
}

export interface OwnerView {
  share_url: string
  workshop: Workshop
  dates: CandidateDate[]
  proposal: Proposal | null
  tally: OwnerTally
  participants: ParticipantRef[]
  availabilities: Availability[]
  recommendation: Recommendation
  enrollments: Enrollment[]
  demo: { demo_mode: boolean; sandbox_mocks: boolean }
}

export interface PublicView {
  workshop: Workshop
  dates: CandidateDate[]
  proposal: Proposal | null
  tally: Tally
}

export interface ParticipantView extends PublicView {
  participant: ParticipantRef
  availability: Availability | null
  enrollment: Enrollment | null
  consent: { version: string; text: string }
}

export interface CreateWorkshopRequest {
  title: string
  description: string
  duration_min: number
  timezone: string
  minimum: number
  capacity: number
  price: string
  currency: string
  payment_window_min?: number
  host_name?: string
  location?: string
  venue_cost?: string
  dates: { starts_at: string; ends_at: string }[]
}

export interface CreateWorkshopResponse {
  workshop: Workshop
  dates: CandidateDate[]
  owner_url: string
  share_url: string
}

export interface EnrollResponse {
  enrollment_id: string
  approve_url: string
  status: EnrollmentStatus
  status_label: string
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly reason?: string

  constructor(status: number, code: string, message: string, reason?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.reason = reason
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, {
      method,
      headers: body === undefined ? { Accept: 'application/json' } : { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
      credentials: 'same-origin',
      cache: 'no-store',
    })
  } catch {
    throw new ApiError(0, 'network_error', 'CommitPool could not be reached. Check your connection and try again.')
  }
  const text = await res.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = null
    }
  }
  if (!res.ok) {
    const e = (data ?? {}) as { error?: string; message?: string; reason?: string }
    throw new ApiError(res.status, e.error ?? 'http_' + res.status, e.message ?? 'Something went wrong. Please try again.', e.reason)
  }
  return data as T
}

const seg = encodeURIComponent

export const api = {
  createWorkshop: (req: CreateWorkshopRequest) => request<CreateWorkshopResponse>('POST', '/api/v1/workshops', req),

  shared: (share: string) => request<PublicView>('GET', `/api/v1/w/${seg(share)}`),
  join: (share: string, displayName: string, email?: string) =>
    request<{ participant_url: string }>('POST', `/api/v1/w/${seg(share)}/participants`, {
      display_name: displayName,
      ...(email ? { email } : {}),
    }),

  owner: (token: string) => request<OwnerView>('GET', `/api/v1/owner/${seg(token)}`),
  openProposal: (token: string, dateId: string) =>
    request<{ proposal: Proposal }>('POST', `/api/v1/owner/${seg(token)}/proposal`, { date_id: dateId }),
  cancelWorkshop: (token: string, reason: string) =>
    request<{ workshop: Workshop }>('POST', `/api/v1/owner/${seg(token)}/cancel`, reason ? { reason } : {}),
  operatorReconcile: (token: string, enrollmentId: string) =>
    request<{ enrollment: Enrollment }>('POST', `/api/v1/owner/${seg(token)}/operator/reconcile`, { enrollment_id: enrollmentId }),
  operatorRetryRefund: (token: string, enrollmentId: string) =>
    request<{ enrollment: Enrollment }>('POST', `/api/v1/owner/${seg(token)}/operator/retry-refund`, { enrollment_id: enrollmentId }),
  operatorClearFlag: (token: string, enrollmentId?: string) =>
    request<{ workshop: Workshop }>('POST', `/api/v1/owner/${seg(token)}/operator/clear-flag`, enrollmentId ? { enrollment_id: enrollmentId } : {}),
  demoCloseNow: (token: string) => request<{ proposal: Proposal }>('POST', `/api/v1/owner/${seg(token)}/demo/close-now`),
  demoFailNextCapture: (token: string, enrollmentId: string) =>
    request<{ enrollment_id: string }>('POST', `/api/v1/owner/${seg(token)}/demo/fail-next-capture`, {
      enrollment_id: enrollmentId,
      mock_code: 'AUTHORIZATION_EXPIRED',
    }),

  participant: (token: string) => request<ParticipantView>('GET', `/api/v1/p/${seg(token)}`),
  submitAvailability: (token: string, message: string) =>
    request<{ availability: Availability }>('POST', `/api/v1/p/${seg(token)}/availability`, { message }),
  correctAvailability: (token: string, id: string, corrected: Interpretation) =>
    request<unknown>('PUT', `/api/v1/p/${seg(token)}/availability/${seg(id)}`, { corrected }),
  enroll: (token: string, consentVersion: string, proposalId: string) =>
    request<EnrollResponse>('POST', `/api/v1/p/${seg(token)}/enrollment`, {
      consent_version: consentVersion,
      proposal_id: proposalId,
    }),
  withdraw: (token: string) => request<Enrollment>('POST', `/api/v1/p/${seg(token)}/withdraw`, {}),
}
