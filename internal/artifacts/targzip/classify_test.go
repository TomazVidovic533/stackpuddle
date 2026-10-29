package targzip

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
)

// The classifier is unexported and decides from the error alone, so it is tested in package with
// the real error os.Root returns for an escape and one error per errno.
func TestChangedOrRawClassifiesByError(t *testing.T) {
	base := t.TempDir()
	inside := filepath.Join(base, "inside")
	if err := os.Mkdir(inside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(base, filepath.Join(inside, "escape")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(inside)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	_, escapeErr := root.Open("escape")
	if escapeErr == nil {
		t.Fatal("os.Root did not refuse an escaping symlink")
	}
	if _, isErrno := errors.AsType[syscall.Errno](escapeErr); isErrno {
		t.Fatalf("os.Root escape error carries an errno; the classifier premise is gone: %v", escapeErr)
	}
	for _, test := range []struct {
		name    string
		err     error
		changed bool
	}{
		{"root escape", escapeErr, true},
		{"ELOOP", &fs.PathError{Op: "openat", Path: "x", Err: syscall.ELOOP}, true},
		{"ENOTDIR", &fs.PathError{Op: "openat", Path: "x", Err: syscall.ENOTDIR}, true},
		{"ENOENT", &fs.PathError{Op: "openat", Path: "x", Err: syscall.ENOENT}, true},
		{"EACCES", &fs.PathError{Op: "openat", Path: "x", Err: syscall.EACCES}, false},
		{"EMFILE", &fs.PathError{Op: "openat", Path: "x", Err: syscall.EMFILE}, false},
		{"ENFILE", &fs.PathError{Op: "openat", Path: "x", Err: syscall.ENFILE}, false},
		{"EIO", &fs.PathError{Op: "openat", Path: "x", Err: syscall.EIO}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := changedOrRaw("content/compose.yaml", test.err)
			failure, isEnvelope := errors.AsType[*envelope.Error](got)
			if test.changed {
				if !isEnvelope || failure.Code != envelope.SourceChanged || !errors.Is(got, test.err) {
					t.Fatalf("want SourceChanged keeping the cause, got %v", got)
				}
				return
			}
			if isEnvelope || got != test.err {
				t.Fatalf("want the raw error, got %v", got)
			}
		})
	}
}
