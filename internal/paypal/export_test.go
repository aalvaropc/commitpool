package paypal

import (
	"net/http"
	"time"
)

// SetNow, BaseURL and HTTPClient let the external tests see into a Client.
func SetNow(c *Client, now func() time.Time) { c.now = now }

func BaseURL(c *Client) string { return c.base }

func HTTPClient(c *Client) *http.Client { return c.hc }
