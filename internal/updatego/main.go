// Command updatego updates go.mod to the latest stable Go release.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/version"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"time"
)

const releasesURL = "https://go.dev/dl/?mode=json"

var (
	goDirective   = regexp.MustCompile(`(?m)^go[ \t]+(\d+\.\d+(?:\.\d+)?)(?:[ \t\r\n]|$)`)
	stableVersion = regexp.MustCompile(`^go\d+\.\d+\.\d+$`)
)

type release struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

func updateModule(content []byte, releases []release) ([]byte, string, error) {
	directives := goDirective.FindAllSubmatchIndex(content, -1)
	if len(directives) != 1 {
		return nil, "", errors.New("expected exactly one stable Go directive in go.mod")
	}
	latest := ""
	for _, candidate := range releases {
		if candidate.Stable && stableVersion.MatchString(candidate.Version) && version.IsValid(candidate.Version) && version.Compare(candidate.Version, latest) > 0 {
			latest = candidate.Version
		}
	}
	if latest == "" {
		return nil, "", errors.New("go release feed contains no stable releases")
	}
	start, end := directives[0][2], directives[0][3]
	current := string(content[start:end])
	if version.Compare(latest, "go"+current) <= 0 {
		return content, current, nil
	}
	updated := append([]byte(nil), content[:start]...)
	updated = append(updated, latest[2:]...)
	updated = append(updated, content[end:]...)
	return updated, latest[2:], nil
}

func run(client *http.Client, output io.Writer) (err error) {
	original, err := os.ReadFile("go.mod")
	if err != nil {
		return err
	}
	response, err := client.Get(releasesURL)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, response.Body.Close()) }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("go release feed returned HTTP %d", response.StatusCode)
	}
	// Bound memory use when decoding the supported-release feed.
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return err
	}
	var releases []release
	if err := json.Unmarshal(data, &releases); err != nil {
		return err
	}
	updated, selected, err := updateModule(original, releases)
	if err != nil {
		return err
	}
	if !bytes.Equal(updated, original) {
		// #nosec G703 -- The destination is the literal go.mod; release data only affects file contents.
		if err := os.WriteFile("go.mod", updated, 0o600); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(output, selected)
	return err
}

func main() {
	if err := run(&http.Client{Timeout: 30 * time.Second}, os.Stdout); err != nil {
		log.Fatal(err)
	}
}
