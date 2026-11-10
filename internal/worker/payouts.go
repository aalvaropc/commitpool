package worker

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/notice"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/present"
	"github.com/aalvaropc/commitpool/internal/store"
)

const (
	payoutSendLimit        = 25 * 24 * time.Hour
	payoutCheckGenerations = 5

	IssuePayoutTooOld    = "PAYOUT_TOO_OLD_TO_SEND"
	IssueDuplicateBatch  = "DUPLICATE_SENDER_BATCH_ID"
	payoutEmailSubject   = "You have a payout from CommitPool"
	payoutCreatedBucket  = "created:"
	payoutPeriodicBucket = "periodic:"
	payoutWebhookBucket  = "webhook:"
	payoutOperatorBucket = "operator:"
)

var payoutCheckGaps = [...]time.Duration{0, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, time.Hour}

var (
	ErrPayoutsOff = errors.New("payouts are switched off on this server")
	ErrNoPayout   = errors.New("the workshop has no payout yet")
)

func PayoutKey(workshopID string) string { return store.JobKey(store.KindPayout, workshopID) }

func PayoutCheckKey(payoutID, bucket string) string {
	return store.JobKey(store.KindPayoutCheck, payoutID+":"+bucket)
}

func parsePayoutCheckKey(key string) (payoutID, bucket string, ok bool) {
	rest, found := strings.CutPrefix(key, store.KindPayoutCheck+":")
	if !found {
		return "", "", false
	}
	payoutID, bucket, ok = strings.Cut(rest, ":")
	return payoutID, bucket, ok && payoutID != "" && bucket != ""
}

func (r *Runner) schedulePayout(ctx context.Context, workshopID string, startsAt time.Time) {
	if !r.cfg.Payouts {
		return
	}
	w, err := r.st.WorkshopByID(ctx, workshopID)
	if err == nil && w.PayoutEmail != "" {
		var created bool
		created, err = r.st.EnqueueJob(ctx, store.KindPayout, PayoutKey(workshopID), &workshopID, nil, nil, startsAt)
		if created {
			r.log.Info("payout scheduled for the start of the class", "workshop_id", workshopID, "run_at", startsAt)
		}
	}
	if err != nil {
		r.log.Error("payout: could not schedule the organizer's payout; the periodic sweep will", "workshop_id", workshopID, "error", err)
	}
}

const payoutWaitPrefix = "payout waits: "

func waitForPayout(log *slog.Logger, reason string) (*int, error) {
	log.Info("payout waits", "reason", reason)
	return nil, &refusal{reason: payoutWaitPrefix + reason}
}

func (r *Runner) payout(ctx context.Context, job *store.Job) (*int, error) {
	if job.WorkshopID == nil {
		return nil, errors.New("payout job has no workshop")
	}
	workshopID := *job.WorkshopID
	log := r.jobLog(job).With("workshop_id", workshopID)
	w, err := r.st.WorkshopByID(ctx, workshopID)
	if err != nil {
		return nil, err
	}
	po, err := r.st.PayoutByWorkshop(ctx, workshopID)
	if err != nil {
		return nil, err
	}
	if po == nil {
		if w.Status == domain.WorkshopCancelled {
			log.Info("workshop cancelled before its payout; nothing to pay out")
			return nil, nil
		}
		if !r.cfg.Payouts {
			return waitForPayout(log, "payouts are switched off")
		}
		row, err := r.beginPayout(ctx, log, workshopID, false)
		var notReady *store.ErrPayoutNotReady
		if errors.As(err, &notReady) {
			return waitForPayout(log, notReady.Reason)
		}
		if err != nil {
			return nil, err
		}
		po = &row
	}
	log = log.With("payout_id", po.ID)
	if po.Status != domain.PayoutCreated {
		log.Info("payout already sent or settled; checking it instead of sending", "payout_status", po.Status)
		return nil, r.scheduleFirstPayoutCheck(ctx, *po)
	}
	if !r.cfg.Payouts {
		return waitForPayout(log, "payouts are switched off")
	}
	return r.sendPayout(ctx, log, job, w, *po)
}

func (r *Runner) beginPayout(ctx context.Context, log *slog.Logger, workshopID string, early bool) (store.PayoutRow, error) {
	row, _, err := r.st.BeginPayout(ctx, workshopID, r.clock.Now(), early)
	var notReady *store.ErrPayoutNotReady
	if !errors.As(err, &notReady) || notReady.Reason != domain.PayoutFeesMissing {
		return row, err
	}
	missing, err := r.st.PaidAttemptsMissingFees(ctx, workshopID)
	if err != nil {
		return store.PayoutRow{}, err
	}
	moving := false
	for _, a := range missing {
		alog := log.With("attempt_id", a.ID, "capture_id", deref(a.CaptureID))
		c, err := r.pp.GetCapture(ctx, *a.CaptureID)
		if err != nil {
			alog.Warn("payout: PayPal did not return a capture whose fees are missing", "error", err)
			continue
		}
		if c.Status != paypal.StatusCompleted {
			alog.Warn("payout: PayPal no longer reports a paid capture completed; reconciling it before paying out", "capture_status", c.Status)
			now := r.clock.Now()
			key := reconcilePrefix(a.ID) + "payout:" + store.Minute(now)
			if err := r.Enqueue(ctx, store.KindReconcile, key, &workshopID, &a.ID, nil, now); err != nil {
				return store.PayoutRow{}, err
			}
			moving = true
			continue
		}
		_, problem, err := r.recordFees(ctx, a.ID, c)
		if err != nil {
			return store.PayoutRow{}, err
		}
		if problem != "" {
			alog.Warn("payout: fees not recorded: " + problem)
		}
	}
	if moving {
		return store.PayoutRow{}, &store.ErrPayoutNotReady{Reason: domain.PayoutInFlight}
	}
	row, _, err = r.st.BeginPayout(ctx, workshopID, r.clock.Now(), early)
	return row, err
}

func (r *Runner) sendPayout(ctx context.Context, log *slog.Logger, job *store.Job, w store.WorkshopRow, po store.PayoutRow) (*int, error) {
	now := r.clock.Now()
	if now.Sub(po.CreatedAt) > payoutSendLimit {
		log.Error("payout created more than 25 days ago and never confirmed sent; PayPal may no longer recognise its sender_batch_id, so it is not sent again",
			"created_at", po.CreatedAt)
		if _, err := r.st.MarkPayoutUnsent(ctx, po.ID, domain.PayoutUnknown, IssuePayoutTooOld, "", now); err != nil {
			return nil, err
		}
		return nil, &refusal{reason: "payout too old to send under its sender_batch_id", issue: IssuePayoutTooOld}
	}
	amount := domain.Money{Minor: po.AmountMinor, Currency: po.Currency}
	batch, err := r.pp.CreatePayout(ctx, paypal.CreatePayoutParams{
		RequestID:     job.IdempotencyKey,
		SenderBatchID: po.SenderBatchID,
		EmailSubject:  payoutEmailSubject,
		EmailMessage:  "What your participants paid for " + w.Title + ", less PayPal's fees.",
		Receiver:      po.Receiver,
		Amount:        paypal.PayoutAmount{Value: amount.String(), Currency: po.Currency},
		Note:          "CommitPool payout for " + w.Title,
		SenderItemID:  po.WorkshopID,
	})
	if err == nil {
		sent, err := r.st.MarkPayoutSent(ctx, po.ID, batch.BatchHeader.PayoutBatchID, batch.BatchHeader.BatchStatus, now)
		if err != nil {
			return nil, err
		}
		log.Info("payout sent to PayPal", "payout_batch_id", batch.BatchHeader.PayoutBatchID, "batch_status", batch.BatchHeader.BatchStatus,
			"amount", amount.String(), "currency", po.Currency)
		return nil, r.scheduleFirstPayoutCheck(ctx, sent)
	}
	if pe := paypal.DuplicatePayout(err); pe != nil {
		return r.duplicatePayout(ctx, log, po, pe, now)
	}
	if pe := paypal.Definitive(err); pe != nil {
		issue := pe.Issue
		if issue == "" {
			issue = pe.Name
		}
		log.Error("PayPal refused the payout", "http_status", pe.HTTPStatus, "issue", issue, "debug_id", pe.DebugID)
		if _, err := r.st.MarkPayoutUnsent(ctx, po.ID, domain.PayoutFailed, issue, pe.DebugID, now); err != nil {
			return nil, err
		}
		return refused(pe)
	}
	log.Warn("PayPal's answer to the payout is unknown; it is sent again under the same sender_batch_id", "error", err)
	return failed(err)
}

func (r *Runner) duplicatePayout(ctx context.Context, log *slog.Logger, po store.PayoutRow, pe *paypal.Error, now time.Time) (*int, error) {
	if id := pe.ExistingPayoutBatchID(); id != "" {
		log.Warn("PayPal already holds this payout's batch; recording it instead of sending again", "payout_batch_id", id, "debug_id", pe.DebugID)
		sent, err := r.st.MarkPayoutSent(ctx, po.ID, id, "", now)
		if err != nil {
			return nil, err
		}
		return statusOf(pe), r.scheduleFirstPayoutCheck(ctx, sent)
	}
	log.Error("PayPal says this payout's sender_batch_id was used but not by which batch; it is never sent again and needs an operator",
		"sender_batch_id", po.SenderBatchID, "debug_id", pe.DebugID)
	if _, err := r.st.MarkPayoutUnsent(ctx, po.ID, domain.PayoutUnknown, IssueDuplicateBatch, pe.DebugID, now); err != nil {
		return nil, err
	}
	return refused(pe)
}

func (r *Runner) scheduleFirstPayoutCheck(ctx context.Context, po store.PayoutRow) error {
	if po.PayoutBatchID == nil {
		return nil
	}
	_, err := r.st.EnqueueJob(ctx, store.KindPayoutCheck, PayoutCheckKey(po.ID, payoutCreatedBucket+"1"), &po.WorkshopID, nil, nil, r.clock.Now())
	return err
}

func (r *Runner) payoutCheck(ctx context.Context, job *store.Job) (*int, error) {
	id, bucket, ok := parsePayoutCheckKey(job.IdempotencyKey)
	if !ok {
		return nil, &refusal{reason: "payout check key is malformed"}
	}
	po, err := r.st.PayoutByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if po == nil {
		return nil, &refusal{reason: "no such payout"}
	}
	log := r.jobLog(job).With("payout_id", po.ID, "workshop_id", po.WorkshopID)
	if po.PayoutBatchID == nil {
		log.Info("payout has no PayPal batch to read yet")
		return nil, nil
	}
	batch, err := r.pp.GetPayoutBatch(ctx, *po.PayoutBatchID)
	if err != nil {
		log.Warn("PayPal did not return the payout batch; reading it again later", "payout_batch_id", *po.PayoutBatchID, "error", err)
		return failed(err)
	}
	check := store.PayoutCheck{BatchStatus: batch.BatchHeader.BatchStatus}
	if item := batch.Item(po.WorkshopID); item != nil {
		check.ItemID, check.ItemStatus, check.TransactionID = item.PayoutItemID, item.TransactionStatus, item.TransactionID
		if item.Errors != nil {
			check.ItemIssue = item.Errors.Name
		}
	}
	check.Status = domain.PayoutState(check.BatchStatus, check.ItemStatus)
	now := r.clock.Now()
	before, after, err := r.st.RecordPayoutCheck(ctx, po.ID, check, now)
	if err != nil {
		return nil, err
	}
	if before.Status != after.Status {
		log.Info("payout moved", "from", before.Status, "to", after.Status, "batch_status", check.BatchStatus, "item_status", check.ItemStatus)
	}
	switch {
	case after.Status == domain.PayoutSucceeded && before.Status != domain.PayoutSucceeded:
		r.notifyPaidOut(ctx, after)
	case after.Status == domain.PayoutFailed && before.Status != domain.PayoutFailed:
		log.Error("PayPal reports the payout failed", "batch_status", check.BatchStatus, "item_status", check.ItemStatus, "issue", check.ItemIssue)
	}
	if after.Status.Final() {
		return nil, nil
	}
	return nil, r.nextCreatedCheck(ctx, after, bucket, now)
}

func (r *Runner) nextCreatedCheck(ctx context.Context, po store.PayoutRow, bucket string, now time.Time) error {
	rest, ok := strings.CutPrefix(bucket, payoutCreatedBucket)
	if !ok {
		return nil
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 1 || n >= payoutCheckGenerations {
		return nil
	}
	key := PayoutCheckKey(po.ID, payoutCreatedBucket+strconv.Itoa(n+1))
	_, err = r.st.EnqueueJob(ctx, store.KindPayoutCheck, key, &po.WorkshopID, nil, nil, now.Add(payoutCheckGaps[n]))
	return err
}

func (r *Runner) notifyPaidOut(ctx context.Context, po store.PayoutRow) {
	w, err := r.st.WorkshopByID(ctx, po.WorkshopID)
	if err == nil && w.OwnerEmail != "" {
		var d notice.Data
		if d, err = r.noticeData(ctx, w); err == nil {
			money := func(minor int64) string { return present.Price(domain.Money{Minor: minor, Currency: po.Currency}) }
			d.Name, d.Paid, d.Receiver = w.HostName, po.Seats, po.Receiver
			d.PaidOut, d.Collected, d.Fees = money(po.AmountMinor), money(po.GrossMinor), money(po.FeeMinor)
			err = r.queueNotice(ctx, notice.OwnerPaidOut, d, store.Notification{
				WorkshopID: po.WorkshopID, To: w.OwnerEmail, Key: "notify:" + NotifyOwnerPaidOut + ":" + po.WorkshopID,
			})
		}
	}
	if err != nil {
		r.log.Error("notify: could not queue the organizer's payout notice", "workshop_id", po.WorkshopID, "error", err)
	}
}

func (r *Runner) StartPayoutNow(ctx context.Context, workshopID string) (store.PayoutRow, error) {
	if !r.cfg.Payouts {
		return store.PayoutRow{}, ErrPayoutsOff
	}
	log := r.log.With("workshop_id", workshopID)
	if _, err := r.beginPayout(ctx, log, workshopID, true); err != nil {
		return store.PayoutRow{}, err
	}
	now := r.clock.Now()
	key := PayoutKey(workshopID)
	if _, err := r.st.EnqueueJob(ctx, store.KindPayout, key, &workshopID, nil, nil, now); err != nil {
		return store.PayoutRow{}, err
	}
	if _, err := r.st.RearmJob(ctx, key, now); err != nil {
		return store.PayoutRow{}, err
	}
	if _, err := r.st.AdvanceJob(ctx, key, now); err != nil {
		return store.PayoutRow{}, err
	}
	log.Info("demo: class started now; paying the organizer", "key", key)
	if err := r.RunKey(ctx, key); err != nil {
		return store.PayoutRow{}, err
	}
	return r.checkCreated(ctx, workshopID)
}

func (r *Runner) checkCreated(ctx context.Context, workshopID string) (store.PayoutRow, error) {
	po, err := r.payoutByWorkshop(ctx, workshopID)
	if err != nil {
		return store.PayoutRow{}, err
	}
	if po.PayoutBatchID != nil {
		if err := r.RunKey(ctx, PayoutCheckKey(po.ID, payoutCreatedBucket+"1")); err != nil && !errors.Is(err, store.ErrNotFound) {
			return store.PayoutRow{}, err
		}
	}
	return r.payoutByWorkshop(ctx, workshopID)
}

func (r *Runner) CheckPayoutNow(ctx context.Context, workshopID string) (store.PayoutRow, error) {
	po, err := r.payoutByWorkshop(ctx, workshopID)
	if err != nil || po.PayoutBatchID == nil {
		return po, err
	}
	key := PayoutCheckKey(po.ID, payoutOperatorBucket+store.Minute(r.clock.Now()))
	if _, err := r.st.EnqueueJob(ctx, store.KindPayoutCheck, key, &po.WorkshopID, nil, nil, r.clock.Now()); err != nil {
		return store.PayoutRow{}, err
	}
	if err := r.RunKey(ctx, key); err != nil {
		return store.PayoutRow{}, err
	}
	return r.payoutByWorkshop(ctx, workshopID)
}

func (r *Runner) payoutByWorkshop(ctx context.Context, workshopID string) (store.PayoutRow, error) {
	po, err := r.st.PayoutByWorkshop(ctx, workshopID)
	if err != nil {
		return store.PayoutRow{}, err
	}
	if po == nil {
		return store.PayoutRow{}, ErrNoPayout
	}
	return *po, nil
}

type PayoutEvent struct {
	ID, PayoutBatchID, SenderBatchID, PayoutItemID string
}

func (r *Runner) PayoutWebhook(ctx context.Context, ev PayoutEvent) (string, error) {
	po, err := r.st.PayoutByPayPalRef(ctx, ev.PayoutBatchID, ev.SenderBatchID, ev.PayoutItemID)
	if err != nil || po == nil {
		return "", err
	}
	now := r.clock.Now()
	if po.PayoutBatchID == nil && ev.PayoutBatchID != "" {
		_, err := r.st.MarkPayoutSent(ctx, po.ID, ev.PayoutBatchID, "", now)
		switch {
		case errors.Is(err, store.ErrPayoutConflict):
			r.log.Warn("webhook: the payout already holds another PayPal batch; the event's batch is not recorded",
				"payout_id", po.ID, "payout_batch_id", ev.PayoutBatchID, "event_id", ev.ID)
		case err != nil:
			return "", err
		default:
			r.log.Info("webhook: recorded the payout's PayPal batch", "payout_id", po.ID, "payout_batch_id", ev.PayoutBatchID, "event_id", ev.ID)
		}
	}
	_, err = r.st.EnqueueJob(ctx, store.KindPayoutCheck, PayoutCheckKey(po.ID, payoutWebhookBucket+ev.ID), &po.WorkshopID, nil, nil, now)
	return po.ID, err
}

func (r *Runner) sweepPayouts(ctx context.Context, now time.Time) error {
	var errs []error
	if r.cfg.Payouts {
		due, err := r.st.DuePayouts(ctx, now)
		errs = append(errs, err)
		for _, id := range due {
			errs = append(errs, r.duePayout(ctx, id, now))
		}
	}
	open, err := r.st.PayoutsToCheck(ctx)
	errs = append(errs, err)
	for _, po := range open {
		key := PayoutCheckKey(po.ID, payoutPeriodicBucket+now.UTC().Format(hourLayout))
		_, err := r.st.EnqueueJob(ctx, store.KindPayoutCheck, key, &po.WorkshopID, nil, nil, now)
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

const payoutSlowWait = time.Hour

func (r *Runner) duePayout(ctx context.Context, workshopID string, now time.Time) error {
	key := PayoutKey(workshopID)
	created, err := r.st.EnqueueJob(ctx, store.KindPayout, key, &workshopID, nil, nil, now)
	if err != nil || created {
		if created {
			r.log.Info("payout due; running it", "workshop_id", workshopID, "key", key)
		}
		return err
	}
	j, err := r.st.JobByKey(ctx, key)
	if err != nil || j == nil || j.Status != store.JobFailed {
		return err
	}
	runAt := now
	if domain.PayoutWaitIsLong(strings.TrimPrefix(deref(j.LastError), payoutWaitPrefix)) {
		runAt = now.Add(payoutSlowWait)
	}
	rearmed, err := r.st.RearmJob(ctx, key, runAt)
	if rearmed {
		r.log.Info("payout waited; trying it again", "workshop_id", workshopID, "key", key, "run_at", runAt)
	}
	return err
}
