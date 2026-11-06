package paypal

import (
	"net/http"
	"time"
)

func SetNow(c *Client, now func() time.Time) { c.now = now }

func BaseURL(c *Client) string { return c.base }

func HTTPClient(c *Client) *http.Client { return c.hc }
