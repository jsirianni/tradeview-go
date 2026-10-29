package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestUpdateModule(t *testing.T) {
	for _, test := range []struct {
		name     string
		module   string
		releases []release
		want     string
		selected string
		wantErr  bool
	}{
		{"numeric order", "go 1.27.3\n", []release{{"go1.27.9", true}, {"go1.27.10", true}}, "go 1.27.10\n", "1.27.10", false},
		{"new minor", "module example.com/test\r\n\r\ngo 1.27.3 // minimum\r\nrequire example.com/lib v1.0.0\r\n", []release{{"go1.28.0", true}}, "module example.com/test\r\n\r\ngo 1.28.0 // minimum\r\nrequire example.com/lib v1.0.0\r\n", "1.28.0", false},
		{"prereleases", "go 1.27.3\n", []release{{"go1.28rc1", false}, {"go1.29.0rc1", true}, {"go1.27.4", true}}, "go 1.27.4\n", "1.27.4", false},
		{"current", "go 1.27.3\n", []release{{"go1.27.3", true}}, "go 1.27.3\n", "1.27.3", false},
		{"no downgrade", "go 1.28.0\n", []release{{"go1.27.3", true}}, "go 1.28.0\n", "1.28.0", false},
		{"short directive", "go 1.27\n", []release{{"go1.27.3", true}}, "go 1.27.3\n", "1.27.3", false},
		{"no stable release", "go 1.27.3\n", []release{{"go1.28rc1", false}}, "", "", true},
		{"empty feed", "go 1.27.3\n", nil, "", "", true},
		{"missing directive", "module example.com/test\n", []release{{"go1.27.4", true}}, "", "", true},
		{"duplicate directive", "go 1.27.3\ngo 1.27.4\n", []release{{"go1.27.4", true}}, "", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, selected, err := updateModule([]byte(test.module), test.releases)
			if (err != nil) != test.wantErr || string(got) != test.want || selected != test.selected {
				t.Fatalf("updateModule() = %q, %q, %v; want %q, %q, error=%v", got, selected, err, test.want, test.selected, test.wantErr)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestRun(t *testing.T) {
	for _, test := range []struct {
		name         string
		body         string
		status       int
		transportErr error
		want         string
		wantErr      bool
	}{
		{"update", `[{"version":"go1.28.0","stable":true}]`, 200, nil, "1.28.0", false},
		{"no change", `[{"version":"go1.27.3","stable":true}]`, 200, nil, "1.27.3", false},
		{"HTTP error", `[]`, 503, nil, "1.27.3", true},
		{"bad JSON", `{`, 200, nil, "1.27.3", true},
		{"empty feed", `[]`, 200, nil, "1.27.3", true},
		{"transport error", "", 0, errors.New("offline"), "1.27.3", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.WriteFile("go.mod", []byte("go 1.27.3\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.String() != releasesURL {
					t.Fatalf("unexpected URL: %s", request.URL)
				}
				if test.transportErr != nil {
					return nil, test.transportErr
				}
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})}
			var output bytes.Buffer
			err := run(client, &output)
			if (err != nil) != test.wantErr {
				t.Fatalf("run() error = %v; want error=%v", err, test.wantErr)
			}
			got, err := os.ReadFile("go.mod")
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "go "+test.want+"\n" {
				t.Fatalf("go.mod = %q", got)
			}
			if !test.wantErr && output.String() != test.want+"\n" {
				t.Fatalf("output = %q", output.String())
			}
			if test.wantErr && output.Len() != 0 {
				t.Fatalf("reported a version after failure: %q", output.String())
			}
		})
	}
}
