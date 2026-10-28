package tradeview

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// defaultHTTPClient keeps credentials and replayable bodies on their original
// origin, while retaining downgrade protection and net/http's redirect limit.
func defaultHTTPClient() *http.Client {
	return &http.Client{CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && request.URL.Scheme == "http" {
			return errors.New("tradeview: refusing HTTPS-to-HTTP redirect")
		}
		if len(via) >= 10 {
			return errors.New("tradeview: stopped after 10 redirects")
		}
		if len(via) > 0 && !sameOrigin(via[0].URL, request.URL) {
			return errors.New("tradeview: refusing cross-origin redirect")
		}
		return nil
	}}
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && effectivePort(a) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
	}
}
