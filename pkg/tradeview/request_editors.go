package tradeview

import (
	"context"
	"errors"
	"net/http"
)

// applyRequestEditors owns the current request body until dispatch can begin.
// Keep its concrete type and optional interfaces visible to public editors.
// Editors own bodies they displace and must clear a body they already closed.
func applyRequestEditors(ctx context.Context, request *http.Request, groups ...[]RequestEditorFn) error {
	for _, editors := range groups {
		for _, editor := range editors {
			if err := editor(ctx, request); err != nil {
				if closeErr := closeRequestBody(request); closeErr != nil {
					return errors.Join(err, closeErr)
				}
				return err
			}
		}
	}
	return nil
}

// Clear ownership before closing, including when Close returns an error. A
// later editor failure must not try to close the consumed body again.
func closeRequestBody(request *http.Request) error {
	body := request.Body
	request.Body = nil
	request.GetBody = nil
	if body == nil {
		return nil
	}
	return body.Close()
}
