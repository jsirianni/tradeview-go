package tradeview

import (
	"fmt"
	"net/http"
)

// defaultHTTPClient protects credentials attached by request editors from a
// downgrade at any redirect hop, while retaining net/http's redirect limit.
func defaultHTTPClient() *http.Client {
	return &http.Client{CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && request.URL.Scheme == "http" {
			return fmt.Errorf("tradeview: refusing HTTPS-to-HTTP redirect")
		}
		if len(via) >= 10 {
			return fmt.Errorf("tradeview: stopped after 10 redirects")
		}
		return nil
	}}
}
