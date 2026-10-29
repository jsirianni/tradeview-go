package tradeview

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"sync"
)

// applyRequestEditors owns request bodies until dispatch can begin. Editors may
// close or replace the body; retain each body they receive so failures also
// clean up a displaced body. Wrappers make cleanup safe when an editor already
// closed its input or a replacement delegates Close to an earlier body.
func applyRequestEditors(ctx context.Context, request *http.Request, groups ...[]RequestEditorFn) (resultErr error) {
	var bodies []*editorRequestBody
	track := func() {
		if request.Body == nil || request.Body == http.NoBody {
			return
		}
		for _, body := range bodies {
			if request.Body == body {
				return
			}
		}
		body := &editorRequestBody{ReadCloser: request.Body}
		bodies = append(bodies, body)
		request.Body = body
	}
	defer func() {
		if resultErr != nil {
			track() // An editor may replace or remove its input before failing.
			for _, body := range slices.Backward(bodies) {
				if err := body.Close(); err != nil {
					resultErr = errors.Join(resultErr, err)
				}
			}
			return
		}
		// Restore the final body's concrete type and normal HTTP ownership. A
		// replacement that wraps an earlier body may still hold its guard.
		for _, body := range bodies {
			if request.Body == body {
				request.Body = body.ReadCloser
				break
			}
		}
	}()
	for _, editors := range groups {
		for _, editor := range editors {
			track()
			if err := editor(ctx, request); err != nil {
				return err
			}
		}
	}
	return nil
}

type editorRequestBody struct {
	io.ReadCloser
	once sync.Once
	err  error
}

func (body *editorRequestBody) Close() error {
	body.once.Do(func() { body.err = body.ReadCloser.Close() })
	return body.err
}
