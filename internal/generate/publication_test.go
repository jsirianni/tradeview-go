package generate

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

var generatedPaths = []string{"pkg/tradeview/api.gen.go", "api/operations.json"}

func TestGeneratePreservesOutputsOnLateFailures(t *testing.T) {
	subprocessErr := errors.New("generator subprocess failed after writing partial output")
	for _, scenario := range []struct {
		name string
		want string
		run  func(string, string, string) error
	}{
		{name: "subprocess", want: subprocessErr.Error(), run: func(_, _, output string) error {
			if err := os.WriteFile(output, []byte("package partial\n"), 0600); err != nil {
				return err
			}
			return subprocessErr
		}},
		{name: "malformed generated source", want: "expected", run: func(_, _, output string) error {
			return os.WriteFile(output, []byte("package !"), 0600)
		}},
		{name: "subprocess exit", want: "run oapi-codegen", run: runCodegen},
		{name: "late symbol rewrite", want: "MissingSymbolInfoModel is not a generated struct", run: runCodegen},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root, original := generationFixture(t)
			if scenario.name == "late symbol rewrite" {
				path := filepath.Join(root, "api", "overlay.yaml")
				data := readGenerationFile(t, path)
				data = bytes.ReplaceAll(data, []byte("- SymbolInfoArrays"), []byte("- MissingSymbolInfoModel"))
				if err := os.WriteFile(path, data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if scenario.name == "subprocess exit" {
				if err := os.WriteFile(filepath.Join(root, "api", "oapi-codegen.yaml"), []byte("package: [invalid, type]\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			var staging string
			err := generateWithRunner(root, func(root, schema, output string) error {
				staging = filepath.Dir(output)
				if filepath.Dir(schema) != staging || strings.HasPrefix(staging, root) {
					t.Fatalf("unexpected staging: schema=%s output=%s", schema, output)
				}
				assertGenerationOutputs(t, root, original)
				err := scenario.run(root, schema, output)
				// Even the real subprocess must write exclusively to staging.
				assertGenerationOutputs(t, root, original)
				return err
			})
			if err == nil || !strings.Contains(err.Error(), scenario.want) {
				t.Fatalf("generation error = %v, want %q", err, scenario.want)
			}
			if scenario.name == "subprocess" && !errors.Is(err, subprocessErr) {
				t.Fatalf("lost subprocess cause: %v", err)
			}
			if scenario.name == "subprocess exit" {
				if _, ok := errors.AsType[*exec.ExitError](err); !ok {
					t.Fatalf("lost subprocess exit error: %v", err)
				}
			}
			assertGenerationOutputs(t, root, original)
			assertStagingRemoved(t, root, staging)
		})
	}
}

func TestGeneratePublishesDeterministicOutputs(t *testing.T) {
	root, expected := generationFixture(t)
	for _, path := range generatedPaths {
		if err := os.WriteFile(filepath.Join(root, path), []byte("previous output\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		var staging string
		if err := generateWithRunner(root, func(root, schema, output string) error {
			staging = filepath.Dir(output)
			return runCodegen(root, schema, output)
		}); err != nil {
			t.Fatal(err)
		}
		assertGenerationOutputs(t, root, expected)
		assertStagingRemoved(t, root, staging)
	}
}

func generationFixture(t *testing.T) (string, map[string][]byte) {
	t.Helper()
	root := t.TempDir()
	paths := append([]string{"go.mod", "go.sum", "api/broker-rest-api.yaml", "api/upstream.json", "api/overlay.yaml", "api/oapi-codegen.yaml"}, generatedPaths...)
	expected := make(map[string][]byte)
	for _, path := range paths {
		data := readGenerationFile(t, filepath.Join("..", "..", path))
		dest := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, data, 0644); err != nil {
			t.Fatal(err)
		}
		expected[path] = data
	}
	return root, expected
}

func readGenerationFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertGenerationOutputs(t *testing.T, root string, expected map[string][]byte) {
	t.Helper()
	for _, path := range generatedPaths {
		if actual := readGenerationFile(t, filepath.Join(root, path)); !bytes.Equal(actual, expected[path]) {
			t.Errorf("generated output %s changed unexpectedly", path)
		}
	}
}

func assertStagingRemoved(t *testing.T, root, staging string) {
	t.Helper()
	if staging != "" {
		if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("generation staging remains at %s: %v", staging, err)
		}
	}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".tradeview-publish-") {
			t.Errorf("publication staging remains: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPublishGeneratedRollsBack(t *testing.T) {
	for _, absentFirst := range []bool{false, true} {
		for _, failAt := range []int{0, 1} {
			t.Run(strings.Join([]string{map[bool]string{false: "existing", true: "new"}[absentFirst], []string{"first", "second"}[failAt]}, "/"), func(t *testing.T) {
				root := t.TempDir()
				outputs := []generatedOutput{{path: filepath.Join(root, "bindings"), data: []byte("new bindings")}, {path: filepath.Join(root, "inventory"), data: []byte("new inventory")}}
				for i, output := range outputs {
					if i != 0 || !absentFirst {
						if err := os.WriteFile(output.path, []byte("original"), 0644); err != nil {
							t.Fatal(err)
						}
					}
				}
				publishErr := errors.New("injected publication failure")
				ops := publicationOps{rename: func(from, to string) error {
					if filepath.Base(from) == "new" && to == outputs[failAt].path {
						return publishErr
					}
					return os.Rename(from, to)
				}, remove: os.Remove}
				if err := publishGenerated(outputs, ops); !errors.Is(err, publishErr) {
					t.Fatalf("publication error = %v", err)
				}
				for i, output := range outputs {
					if i == 0 && absentFirst {
						if _, err := os.Stat(output.path); !errors.Is(err, os.ErrNotExist) {
							t.Errorf("new output survived rollback: %v", err)
						}
					} else if got := string(readGenerationFile(t, output.path)); got != "original" {
						t.Errorf("output after rollback = %q", got)
					}
				}
				assertStagingRemoved(t, root, "")
			})
		}
	}
}

func TestPublishGeneratedReportsRollbackFailure(t *testing.T) {
	for _, existed := range []bool{true, false} {
		t.Run(map[bool]string{true: "restore backup", false: "remove new output"}[existed], func(t *testing.T) {
			root := t.TempDir()
			outputs := []generatedOutput{{path: filepath.Join(root, "bindings"), data: []byte("new bindings")}, {path: filepath.Join(root, "inventory"), data: []byte("new inventory")}}
			if existed {
				if err := os.WriteFile(outputs[0].path, []byte("original"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			publishErr, rollbackErr := errors.New("cannot publish"), errors.New("cannot roll back")
			var backup string
			err := publishGenerated(outputs, publicationOps{rename: func(from, to string) error {
				if filepath.Base(from) == "previous" {
					backup = from
					return rollbackErr
				}
				if to == outputs[1].path {
					return publishErr
				}
				return os.Rename(from, to)
			}, remove: func(string) error { return rollbackErr }})
			if !errors.Is(err, publishErr) || !errors.Is(err, rollbackErr) || !strings.Contains(err.Error(), strconv.Quote(outputs[0].path)) {
				t.Fatalf("lost publication or rollback diagnostics: %v", err)
			}
			if existed {
				if !strings.Contains(err.Error(), strconv.Quote(backup)) || string(readGenerationFile(t, backup)) != "original" {
					t.Fatalf("missing reported recovery backup: %v", err)
				}
				if err := os.RemoveAll(filepath.Dir(backup)); err != nil {
					t.Fatal(err)
				}
			}
			assertStagingRemoved(t, root, "")
		})
	}
}

func TestPublishGeneratedPreflightsAllOutputs(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "bindings"), filepath.Join(root, "inventory")
	if err := os.WriteFile(first, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(second, 0755); err != nil {
		t.Fatal(err)
	}
	err := publishGenerated([]generatedOutput{{path: first, data: []byte("new")}, {path: second}}, publicationOps{rename: os.Rename, remove: os.Remove})
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("accepted invalid output: %v", err)
	}
	if got := string(readGenerationFile(t, first)); got != "original" {
		t.Fatalf("changed first output before preparing second: %q", got)
	}
	assertStagingRemoved(t, root, "")
}

func TestPublishGeneratedPreservesPermissions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bindings")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := publishGenerated([]generatedOutput{{path: path, data: []byte("replacement")}}, publicationOps{rename: os.Rename, remove: os.Remove}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("changed original mode: %v", info.Mode())
	}
	if string(readGenerationFile(t, path)) != "replacement" {
		t.Fatal("replacement was not published")
	}
	assertStagingRemoved(t, root, "")
}
