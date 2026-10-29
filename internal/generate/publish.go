package generate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type generatedOutput struct {
	path string
	data []byte
}

// These operations are passed explicitly so tests can exercise publication and
// rollback failures without changing process-wide state or file permissions.
type publicationOps struct {
	rename func(string, string) error
	remove func(string) error
}

type stagedOutput struct {
	path, dir, next, backup string
	existed, retain         bool
}

// publishGenerated prepares every replacement and backup before changing any
// output. Each rename stays in the destination directory's filesystem. This is
// rollback for ordinary errors, not an atomic multi-file transaction: generation
// must run without concurrent output writers, and a process crash can interrupt
// publication. A failed rollback retains its backup and reports its path.
func publishGenerated(outputs []generatedOutput, ops publicationOps) (result error) {
	staged := make([]stagedOutput, 0, len(outputs))
	defer func() {
		for _, output := range staged {
			if !output.retain {
				if err := os.RemoveAll(output.dir); err != nil {
					result = errors.Join(result, fmt.Errorf("clean publication staging %q: %w", output.dir, err))
				}
			}
		}
	}()
	for _, output := range outputs {
		for _, previous := range staged {
			if output.path == previous.path {
				return fmt.Errorf("duplicate generated output %q", output.path)
			}
		}
		mode := os.FileMode(0644)
		var original []byte
		info, err := os.Lstat(output.path)
		existed := err == nil
		if existed {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("generated output %q is not a regular file", output.path)
			}
			mode = info.Mode().Perm()
			// #nosec G304 -- Fixed generated output under the developer-selected repository root.
			original, err = os.ReadFile(output.path)
			if err != nil {
				return fmt.Errorf("snapshot generated output %q: %w", output.path, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("snapshot generated output %q: %w", output.path, err)
		}
		parent := filepath.Dir(output.path)
		// #nosec G301 -- Generated public source directories are intentionally mode 0755.
		if err := os.MkdirAll(parent, 0755); err != nil {
			return fmt.Errorf("create output directory %q: %w", parent, err)
		}
		dir, err := os.MkdirTemp(parent, ".tradeview-publish-*")
		if err != nil {
			return fmt.Errorf("stage generated output %q: %w", output.path, err)
		}
		entry := stagedOutput{path: output.path, dir: dir, next: filepath.Join(dir, "new"), backup: filepath.Join(dir, "previous"), existed: existed}
		staged = append(staged, entry)
		if err := writePublicationFile(entry.next, output.data, mode); err != nil {
			return fmt.Errorf("stage replacement for %q: %w", output.path, err)
		}
		if existed {
			if err := writePublicationFile(entry.backup, original, mode); err != nil {
				return fmt.Errorf("stage backup for %q: %w", output.path, err)
			}
		}
	}
	for i, output := range staged {
		if err := ops.rename(output.next, output.path); err != nil {
			result = fmt.Errorf("publish generated output %q: %w", output.path, err)
			for j := i - 1; j >= 0; j-- {
				previous := &staged[j]
				var rollbackErr error
				if previous.existed {
					rollbackErr = ops.rename(previous.backup, previous.path)
				} else {
					rollbackErr = ops.remove(previous.path)
					if errors.Is(rollbackErr, os.ErrNotExist) {
						rollbackErr = nil
					}
				}
				if rollbackErr != nil {
					previous.retain = previous.existed
					result = errors.Join(result, fmt.Errorf("rollback generated output %q: %w", previous.path, rollbackErr))
					if previous.existed {
						result = errors.Join(result, fmt.Errorf("original output retained for recovery at %q", previous.backup))
					}
				}
			}
			return result
		}
	}
	return nil
}

func writePublicationFile(path string, data []byte, mode os.FileMode) error {
	// #nosec G703 G306 -- Fixed filename in our private destination staging directory; preserve source permissions or use public-source mode 0644.
	if err := os.WriteFile(path, data, mode); err != nil {
		return err
	}
	// Apply the exact original mode even when the process umask is more restrictive.
	// #nosec G302 -- Preserve the permissions of developer-controlled generated public source.
	return os.Chmod(path, mode)
}
