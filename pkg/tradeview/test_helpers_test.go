package tradeview

import (
	"io"
	"testing"
)

// Nonfatal reporting is safe from HTTP handlers, cleanup callbacks and workers.
func checkTestError(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Error(err)
	}
}

func closeTestBody(t testing.TB, body io.Closer) {
	t.Helper()
	checkTestError(t, body.Close())
}

func writeTestString(t testing.TB, writer io.Writer, value string) {
	t.Helper()
	_, err := io.WriteString(writer, value)
	checkTestError(t, err)
}
