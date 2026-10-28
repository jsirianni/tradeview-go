package generate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

func TestGenerateDecodeErrorsIncludeInputPath(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		content  string
		stage    string
		yamlType bool
	}{
		{name: "metadata syntax", file: "upstream.json", content: "{", stage: "decode upstream metadata"},
		{name: "schema syntax", file: "broker-rest-api.yaml", content: "openapi: [", stage: "decode schema"},
		{name: "overlay syntax", file: "overlay.yaml", content: "number-format: [", stage: "decode overlay"},
		{name: "overlay type", file: "overlay.yaml", content: "number-format: []", stage: "decode overlay", yamlType: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "repo with spaces")
			api := filepath.Join(root, "api")
			if err := os.MkdirAll(api, 0755); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"broker-rest-api.yaml", "upstream.json", "overlay.yaml"} {
				data, err := os.ReadFile(filepath.Join("..", "..", "api", name))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(api, name), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(api, tc.file)
			if err := os.WriteFile(path, []byte(tc.content), 0644); err != nil {
				t.Fatal(err)
			}
			if tc.file == "broker-rest-api.yaml" {
				digest := sha256.Sum256([]byte(tc.content))
				data, err := json.Marshal(map[string]string{"version": "1.21.0", "sha256": hex.EncodeToString(digest[:])})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(api, "upstream.json"), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			err := Generate(root)
			if err == nil || !strings.Contains(err.Error(), tc.stage) || !strings.Contains(err.Error(), strconv.Quote(path)) {
				t.Fatalf("Generate error = %v, want stage %q and path %q", err, tc.stage, path)
			}
			if errors.Unwrap(err) == nil {
				t.Fatalf("decoder cause lost: %v", err)
			}
			if tc.file == "upstream.json" {
				if _, ok := errors.AsType[*json.SyntaxError](err); !ok {
					t.Fatalf("JSON syntax cause lost: %v", err)
				}
			}
			if tc.yamlType {
				if _, ok := errors.AsType[*yaml.TypeError](err); !ok {
					t.Fatalf("YAML type cause lost: %v", err)
				}
			}
			if _, statErr := os.Stat(filepath.Join(root, "pkg")); !os.IsNotExist(statErr) {
				t.Fatalf("malformed inputs wrote package output: %v", statErr)
			}
		})
	}
}
