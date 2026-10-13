package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/afero"
)

// ScopedFs is an afero.Fs that confines every operation to a base directory and
// refuses to follow a symbolic link whose on-disk target resolves outside that
// base. It wraps an *afero.BasePathFs — which already provides the lexical
// confinement — and adds a per-operation scope check on every call that would
// dereference a symlink at the OS layer (open, stat, lstat, chmod, …).
type ScopedFs struct {
	base *afero.BasePathFs
}

var (
	_ afero.Fs      = (*ScopedFs)(nil)
	_ afero.Lstater = (*ScopedFs)(nil)
)

// maxSymlinkHops bounds how many dangling symlinks within() will follow before
// giving up, so a pathological chain cannot loop forever. It mirrors the kernel
// MAXSYMLINKS limit; the operation is rejected once the bound is exceeded.
const maxSymlinkHops = 255

func NewScopedFs(source afero.Fs, path string) *ScopedFs {
	if s, ok := source.(*ScopedFs); ok {
		source = s.base
	}
	return &ScopedFs{base: afero.NewBasePathFs(source, path).(*afero.BasePathFs)}
}

// NewFs builds a user filesystem rooted at path. When followExternal is true it
// returns a bare BasePathFs, so symlinks whose target resolves outside the scope
// are followed; otherwise it returns a ScopedFs that refuses to follow them.
func NewFs(source afero.Fs, path string, followExternal bool) afero.Fs {
	if followExternal {
		return afero.NewBasePathFs(source, path)
	}
	return NewScopedFs(source, path)
}

// BasePath returns the underlying *afero.BasePathFs of a user filesystem built
// by NewFs, whether it is a *ScopedFs or a bare *afero.BasePathFs, or nil if it
// is neither.
func BasePath(fs afero.Fs) *afero.BasePathFs {
	switch f := fs.(type) {
	case *ScopedFs:
		return f.BasePathFs()
	case *afero.BasePathFs:
		return f
	}
	return nil
}

// ResolvedPath returns a path relative to fs's base after resolving symlinks.
// For a path that does not exist yet, it resolves the nearest existing
// ancestor and appends the remaining components. Filesystems without an OS
// base path retain the cleaned virtual path.
func ResolvedPath(filesystem afero.Fs, name string) (string, error) {
	base := BasePath(filesystem)
	if base == nil {
		return filepath.ToSlash(filepath.Clean("/" + name)), nil
	}
	root, resolved, err := resolveBasePath(base, name)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", err
	}
	if relative == "." {
		return "/", nil
	}
	return "/" + filepath.ToSlash(relative), nil
}

func resolveBasePath(base *afero.BasePathFs, name string) (string, string, error) {
	root, err := filepath.EvalSymlinks(afero.FullBaseFsPath(base, "/"))
	if err != nil {
		return "", "", err
	}
	target := afero.FullBaseFsPath(base, name)
	resolved, err := filepath.EvalSymlinks(target)
	var missing []string
	for hops := 0; errors.Is(err, fs.ErrNotExist); {
		if info, statErr := os.Lstat(target); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			hops++
			if hops > maxSymlinkHops {
				return "", "", os.ErrPermission
			}
			dest, readErr := os.Readlink(target)
			if readErr != nil {
				return "", "", readErr
			}
			if !filepath.IsAbs(dest) {
				parent, parentErr := filepath.EvalSymlinks(filepath.Dir(target))
				if parentErr != nil {
					return "", "", parentErr
				}
				dest = filepath.Join(parent, dest)
			}
			target = filepath.Clean(dest)
		} else {
			parent := filepath.Dir(target)
			if parent == target {
				break
			}
			missing = append(missing, filepath.Base(target))
			target = parent
		}
		resolved, err = filepath.EvalSymlinks(target)
	}
	if err != nil {
		return "", "", err
	}
	for i := len(missing) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, missing[i])
	}
	return root, resolved, nil
}

// BasePathFs returns the underlying *afero.BasePathFs.
func (s *ScopedFs) BasePathFs() *afero.BasePathFs { return s.base }

// RealPath resolves a scoped path to the real on-disk path by delegating to
// the underlying BasePathFs. This is needed by callers that need the actual
// filesystem path (e.g. disk.UsageWithContext).
func (s *ScopedFs) RealPath(name string) (string, error) {
	return s.base.RealPath(name)
}

// guard returns an error if name's on-disk target resolves outside the scope.
func (s *ScopedFs) guard(name string) error {
	ok, err := s.within(name)
	if err != nil {
		return err
	}
	if !ok {
		return os.ErrPermission
	}
	return nil
}

// within reports whether the on-disk target of p — after resolving any symbolic
// links — stays within the scoped root. It exists to stop a symlink that lives
// lexically inside the scope but points outside it from being followed for
// reads, writes, or shares.
//
// Paths that do not exist yet (e.g. a brand-new file being created) are
// validated against their nearest existing ancestor, so legitimate new files
// are always allowed. A dangling symlink — a link whose target does not exist
// yet — is the exception: it is followed to where it points and validated
// there, so a write cannot dereference the link to create a file outside the
// scope.
func (s *ScopedFs) within(p string) (bool, error) {
	root, resolved, err := resolveBasePath(s.base, p)
	if err != nil {
		return false, err
	}
	prefix := root
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	return resolved == root || strings.HasPrefix(resolved, prefix), nil
}

func (s *ScopedFs) Create(name string) (afero.File, error) {
	if err := s.guard(name); err != nil {
		return nil, err
	}
	return s.base.Create(name)
}

func (s *ScopedFs) Mkdir(name string, perm os.FileMode) error {
	if err := s.guard(name); err != nil {
		return err
	}
	return s.base.Mkdir(name, perm)
}

func (s *ScopedFs) MkdirAll(path string, perm os.FileMode) error {
	if err := s.guard(path); err != nil {
		return err
	}
	return s.base.MkdirAll(path, perm)
}

func (s *ScopedFs) Open(name string) (afero.File, error) {
	if err := s.guard(name); err != nil {
		return nil, err
	}
	return s.base.Open(name)
}

func (s *ScopedFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if err := s.guard(name); err != nil {
		return nil, err
	}
	return s.base.OpenFile(name, flag, perm)
}

func (s *ScopedFs) Remove(name string) error {
	if err := s.guard(name); err != nil {
		return err
	}
	return s.base.Remove(name)
}

func (s *ScopedFs) RemoveAll(path string) error {
	if err := s.guard(path); err != nil {
		return err
	}
	return s.base.RemoveAll(path)
}

func (s *ScopedFs) Rename(oldname, newname string) error {
	if err := s.guard(oldname); err != nil {
		return err
	}
	if err := s.guard(newname); err != nil {
		return err
	}
	return s.base.Rename(oldname, newname)
}

func (s *ScopedFs) Stat(name string) (os.FileInfo, error) {
	if err := s.guard(name); err != nil {
		return nil, err
	}
	return s.base.Stat(name)
}

func (s *ScopedFs) Name() string { return "ScopedFs" }

func (s *ScopedFs) Chmod(name string, mode os.FileMode) error {
	if err := s.guard(name); err != nil {
		return err
	}
	return s.base.Chmod(name, mode)
}

func (s *ScopedFs) Chown(name string, uid, gid int) error {
	if err := s.guard(name); err != nil {
		return err
	}
	return s.base.Chown(name, uid, gid)
}

func (s *ScopedFs) Chtimes(name string, atime, mtime time.Time) error {
	if err := s.guard(name); err != nil {
		return err
	}
	return s.base.Chtimes(name, atime, mtime)
}

func (s *ScopedFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if err := s.guard(name); err != nil {
		return nil, false, err
	}
	return s.base.LstatIfPossible(name)
}
