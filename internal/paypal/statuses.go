package paypal

// Statuses PayPal reports on orders, authorizations, captures and refunds.
// PayPal reports StatusExpired for an authorization, never for an order.
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

// Issues PayPal reports in an error's details that callers branch on.
const (
	// IssueInvalidResourceID comes with a 404 for a resource PayPal does not
	// know, such as an order dropped unapproved after a few hours.
	IssueInvalidResourceID = "INVALID_RESOURCE_ID"
	// IssueOrderNotApproved is the 422 for authorizing an order the buyer
	// has not approved yet.
	IssueOrderNotApproved = "ORDER_NOT_APPROVED"
)
