package paypal

// Statuses PayPal reports on orders, authorizations, captures and refunds.
// PayPal never reports StatusExpired for an order: it is what the worker
// records once PayPal no longer knows an order the buyer never approved.
const (
	StatusCreated             = "CREATED"
	StatusPayerActionRequired = "PAYER_ACTION_REQUIRED"
	StatusApproved            = "APPROVED"
	StatusCompleted           = "COMPLETED"
	StatusPending             = "PENDING"
	StatusDeclined            = "DECLINED"
	StatusFailed              = "FAILED"
	StatusDenied              = "DENIED"
	StatusVoided              = "VOIDED"
	StatusCaptured            = "CAPTURED"
	StatusRefunded            = "REFUNDED"
	StatusPartiallyRefunded   = "PARTIALLY_REFUNDED"
	StatusCancelled           = "CANCELLED"
	StatusExpired             = "EXPIRED"
)
