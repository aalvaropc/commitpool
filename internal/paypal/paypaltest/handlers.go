package paypaltest

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
)

func (f *Server) token(r *http.Request, body []byte) fakeResponse {
	if _, _, ok := r.BasicAuth(); !ok {
		return jsonResponse(http.StatusUnauthorized, map[string]string{
			"error": "invalid_client", "error_description": "Client Authentication failed",
		})
	}
	form, err := url.ParseQuery(string(body))
	if err != nil || form.Get("grant_type") != "client_credentials" {
		return jsonResponse(http.StatusBadRequest, map[string]string{
			"error": "unsupported_grant_type", "error_description": "Grant type is NULL",
		})
	}
	return jsonResponse(http.StatusOK, map[string]any{
		"access_token": fakeToken, "token_type": "Bearer", "expires_in": 32400,
	})
}

func (f *Server) verifyWebhook(r *http.Request, body []byte) fakeResponse {
	status := "FAILURE"
	if f.verify {
		status = "SUCCESS"
	}
	return jsonResponse(http.StatusOK, map[string]string{"verification_status": status})
}

func (f *Server) createOrder(r *http.Request, body []byte) fakeResponse {
	var req struct {
		Intent        string                `json:"intent"`
		PurchaseUnits []paypal.PurchaseUnit `json:"purchase_units"`
		PaymentSource struct {
			PayPal struct {
				ExperienceContext struct {
					ReturnURL string `json:"return_url"`
					CancelURL string `json:"cancel_url"`
				} `json:"experience_context"`
			} `json:"paypal"`
		} `json:"payment_source"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Intent != "AUTHORIZE" || len(req.PurchaseUnits) != 1 ||
		req.PurchaseUnits[0].Amount.CurrencyCode == "" || req.PurchaseUnits[0].Amount.Value == "" {
		return errorResponse(http.StatusBadRequest, "INVALID_REQUEST", "MALFORMED_REQUEST",
			"Request is not well-formed, syntactically incorrect, or violates schema.")
	}
	unit := req.PurchaseUnits[0]
	ctx := req.PaymentSource.PayPal.ExperienceContext
	o := &fakeOrder{
		id:          newID(),
		status:      orderPayerActionRequired,
		referenceID: unit.ReferenceID,
		customID:    unit.CustomID,
		amount:      unit.Amount,
		returnURL:   ctx.ReturnURL,
		cancelURL:   ctx.CancelURL,
	}
	f.orders[o.id] = o
	return jsonResponse(http.StatusCreated, f.orderView(o, false))
}

func (f *Server) getOrder(r *http.Request, body []byte) fakeResponse {
	o, ok := f.liveOrder(r.PathValue("id"))
	if !ok {
		return notFound()
	}
	return jsonResponse(http.StatusOK, f.orderView(o, true))
}

func (f *Server) authorize(r *http.Request, body []byte) fakeResponse {
	o, ok := f.liveOrder(r.PathValue("id"))
	if !ok || o.status == orderCompleted {
		return notFound()
	}
	if o.status != orderApproved {
		return unprocessable("ORDER_NOT_APPROVED")
	}
	status := authCreated
	if f.nextAuth != "" {
		status, f.nextAuth = f.nextAuth, ""
	}
	a := &fakeAuth{
		Authorization: paypal.Authorization{
			ID:             newID(),
			Status:         status,
			ExpirationTime: time.Now().UTC().Add(29 * 24 * time.Hour).Truncate(time.Second),
			Amount:         o.amount,
		},
		orderID: o.id,
	}
	f.auths[a.ID] = a
	o.authIDs = append(o.authIDs, a.ID)
	o.status = orderCompleted
	return jsonResponse(http.StatusCreated, f.orderView(o, false))
}

func (f *Server) getAuthorization(r *http.Request, body []byte) fakeResponse {
	a, ok := f.auths[r.PathValue("id")]
	if !ok {
		return notFound()
	}
	return jsonResponse(http.StatusOK, a.Authorization)
}

func (f *Server) capture(r *http.Request, body []byte) fakeResponse {
	a, ok := f.auths[r.PathValue("id")]
	if !ok {
		return notFound()
	}
	switch a.Status {
	case authVoided:
		return unprocessable("AUTHORIZATION_VOIDED")
	case authCaptured:
		return unprocessable("AUTHORIZATION_ALREADY_CAPTURED")
	}
	switch code := mockCode(r); code {
	case "AUTHORIZATION_EXPIRED", "TRANSACTION_REFUSED":
		return unprocessable(code)
	}
	cp := &fakeCapture{
		Capture: paypal.Capture{ID: newID(), Status: captureCompleted, Amount: a.Amount, SellerReceivableBreakdown: sellerBreakdown(a.Amount)},
		orderID: a.orderID,
	}
	f.captures[cp.ID] = cp
	o := f.orders[a.orderID]
	o.captureIDs = append(o.captureIDs, cp.ID)
	a.Status = authCaptured
	return jsonResponse(http.StatusCreated, cp.Capture)
}

const (
	feeBasisPoints = 349
	feeFixedMinor  = 49
)

func sellerBreakdown(gross paypal.Money) *paypal.SellerReceivableBreakdown {
	g, err := domain.ParseMoney(gross.Value, gross.CurrencyCode)
	if err != nil {
		return nil
	}
	fee := min(g.Minor, (g.Minor*feeBasisPoints+5000)/10000+feeFixedMinor)
	money := func(minor int64) *paypal.Money {
		return &paypal.Money{CurrencyCode: gross.CurrencyCode, Value: domain.Money{Minor: minor, Currency: g.Currency}.String()}
	}
	return &paypal.SellerReceivableBreakdown{GrossAmount: money(g.Minor), PayPalFee: money(fee), NetAmount: money(g.Minor - fee)}
}

func (f *Server) void(r *http.Request, body []byte) fakeResponse {
	a, ok := f.auths[r.PathValue("id")]
	if !ok {
		return notFound()
	}
	switch a.Status {
	case authCaptured:
		return unprocessable("PREVIOUSLY_CAPTURED")
	case authVoided:
		return unprocessable("PREVIOUSLY_VOIDED")
	}
	a.Status = authVoided
	return fakeResponse{status: http.StatusNoContent}
}

func (f *Server) getCapture(r *http.Request, body []byte) fakeResponse {
	cp, ok := f.captures[r.PathValue("id")]
	if !ok {
		return notFound()
	}
	return jsonResponse(http.StatusOK, cp.Capture)
}

func (f *Server) refund(r *http.Request, body []byte) fakeResponse {
	cp, ok := f.captures[r.PathValue("id")]
	if !ok {
		return notFound()
	}
	if cp.Status == captureRefunded {
		return unprocessable("CAPTURE_FULLY_REFUNDED")
	}
	if mockCode(r) == "REFUND_AMOUNT_EXCEEDED" {
		return unprocessable("REFUND_AMOUNT_EXCEEDED")
	}
	rf := &fakeRefund{
		Refund:  paypal.Refund{ID: newID(), Status: refundCompleted, Amount: cp.Amount},
		orderID: cp.orderID,
	}
	f.refunds[rf.ID] = rf
	o := f.orders[cp.orderID]
	o.refundIDs = append(o.refundIDs, rf.ID)
	cp.Status = captureRefunded
	return jsonResponse(http.StatusCreated, rf.Refund)
}

func (f *Server) getRefund(r *http.Request, body []byte) fakeResponse {
	rf, ok := f.refunds[r.PathValue("id")]
	if !ok {
		return notFound()
	}
	return jsonResponse(http.StatusOK, rf.Refund)
}

func (f *Server) liveOrder(id string) (*fakeOrder, bool) {
	o, ok := f.orders[id]
	if !ok || o.expired {
		return nil, false
	}
	return o, true
}

func (f *Server) orderView(o *fakeOrder, forGet bool) paypal.Order {
	unit := paypal.PurchaseUnit{ReferenceID: o.referenceID, CustomID: o.customID, Amount: o.amount}
	if forGet && o.reported != nil {
		unit.Amount = *o.reported
	}
	for _, id := range o.authIDs {
		unit.Payments.Authorizations = append(unit.Payments.Authorizations, f.auths[id].Authorization)
	}
	for _, id := range o.captureIDs {
		unit.Payments.Captures = append(unit.Payments.Captures, f.captures[id].Capture)
	}
	for _, id := range o.refundIDs {
		unit.Payments.Refunds = append(unit.Payments.Refunds, f.refunds[id].Refund)
	}
	links := []paypal.Link{{Rel: "self", Href: f.URL + "/v2/checkout/orders/" + o.id, Method: "GET"}}
	if o.status == orderPayerActionRequired {
		links = append(links, paypal.Link{Rel: "payer-action", Href: f.URL + "/approve?order=" + o.id, Method: "GET"})
	}
	return paypal.Order{ID: o.id, Status: o.status, Links: links, PurchaseUnits: []paypal.PurchaseUnit{unit}}
}

func mockCode(r *http.Request) string {
	var m struct {
		Code string `json:"mock_application_codes"`
	}
	if json.Unmarshal([]byte(r.Header.Get(headerMockResponse)), &m) != nil {
		return ""
	}
	return m.Code
}

var approveTemplate = template.Must(template.New("approve").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Fake PayPal checkout</title></head>
<body>
<h1>Fake PayPal checkout</h1>
<p>Authorize {{.Value}} {{.Currency}} for order {{.ID}}.</p>
<form method="post" action="` + fakePrefix + `/approve">
<input type="hidden" name="order" value="{{.ID}}">
<button type="submit">Approve</button>
</form>
<p><a href="` + fakePrefix + `/cancel?order={{.ID}}">Cancel</a></p>
</body>
</html>
`))

func (f *Server) approvePage(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	o, ok := f.liveOrder(r.URL.Query().Get("order"))
	var data struct{ ID, Value, Currency string }
	if ok {
		data.ID, data.Value, data.Currency = o.id, o.amount.Value, o.amount.CurrencyCode
	}
	f.mu.Unlock()
	if !ok {
		http.Error(w, "unknown order", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = approveTemplate.Execute(w, data)
}

func (f *Server) approveSubmit(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	o, ok := f.liveOrder(r.FormValue("order"))
	var returnURL, id string
	if ok {
		f.approveLocked(o)
		returnURL, id = o.returnURL, o.id
	}
	f.mu.Unlock()
	if !ok {
		http.Error(w, "unknown order", http.StatusNotFound)
		return
	}
	redirectWithToken(w, returnURL, "token="+id+"&PayerID=FAKE")
}

func (f *Server) cancelPage(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	o, ok := f.liveOrder(r.URL.Query().Get("order"))
	var cancelURL, id string
	if ok {
		cancelURL, id = o.cancelURL, o.id
	}
	f.mu.Unlock()
	if !ok {
		http.Error(w, "unknown order", http.StatusNotFound)
		return
	}
	redirectWithToken(w, cancelURL, "token="+id)
}

func redirectWithToken(w http.ResponseWriter, target, query string) {
	if target == "" {
		http.Error(w, "the order has no return URL", http.StatusBadRequest)
		return
	}
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	w.Header().Set("Location", target+sep+query)
	w.WriteHeader(http.StatusFound)
}
