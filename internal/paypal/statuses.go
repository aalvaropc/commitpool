package paypal

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

const (
	IssueInvalidResourceID = "INVALID_RESOURCE_ID"
	IssueOrderNotApproved  = "ORDER_NOT_APPROVED"
)
