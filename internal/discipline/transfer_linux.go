package discipline

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

type StorageConfig struct{ Mount, Source, ReadyFile string }

type mountRecord struct {
	Target  string `json:"target"`
	Source  string `json:"source"`
	Type    string `json:"fstype"`
	Options string `json:"options"`
	ID      uint64 `json:"id"`
}

func (c StorageConfig) validate() error {
	for _, p := range []string{c.Mount, c.ReadyFile} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p || p == "/" {
			return errors.New("storage paths must be absolute and canonical")
		}
	}
	if c.Source == "" {
		return errors.New("expected NFS source is required")
	}
	return nil
}

func validateMount(c StorageConfig, m mountRecord, id uint64) error {
	options := "," + m.Options + ","
	if m.Target != c.Mount || m.Source != c.Source || (m.Type != "nfs" && m.Type != "nfs4") || m.ID != id || id == 0 {
		return errors.New("expected visible NFS mount is absent or replaced")
	}
	for _, option := range []string{"rw", "hard", "sec=sys"} {
		if !strings.Contains(options, ","+option+",") {
			return fmt.Errorf("NFS mount requires %s", option)
		}
	}
	for _, option := range []string{"ro", "soft", "softerr", "softreval"} {
		if strings.Contains(options, ","+option+",") {
			return errors.New("unsafe NFS mount options")
		}
	}
	return nil
}

func openDirectory(path string) (*os.File, error) {
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func mountID(f *os.File) (uint64, error) {
	var st unix.Statx_t
	if err := unix.Statx(int(f.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &st); err != nil {
		return 0, err
	}
	if st.Mask&unix.STATX_MNT_ID == 0 {
		return 0, errors.New("kernel does not report mount identity")
	}
	return st.Mnt_id, nil
}

func childDirectory(parent *os.File, name string, create bool) (*os.File, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") {
		return nil, errors.New("invalid directory component")
	}
	if create {
		if err := unix.Mkdirat(int(parent.Fd()), name, 0755); err != nil && !errors.Is(err, unix.EEXIST) {
			return nil, err
		}
	}
	fd, err := unix.Openat2(int(parent.Fd()), name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	if create {
		if err := parent.Sync(); err != nil {
			f.Close()
			return nil, err
		}
	}
	return f, nil
}

func (c StorageConfig) pin(ctx context.Context) (*os.File, error) {
	marker, err := unix.Openat2(unix.AT_FDCWD, c.ReadyFile, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return nil, errors.New("storage readiness marker is absent or unsafe")
	}
	f := os.NewFile(uintptr(marker), c.ReadyFile)
	info, statErr := f.Stat()
	f.Close()
	if statErr != nil || !info.Mode().IsRegular() {
		return nil, errors.New("storage readiness marker must be regular")
	}
	root, err := openDirectory(c.Mount)
	if err != nil {
		return nil, err
	}
	if err := c.check(ctx, root); err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}

func (c StorageConfig) check(ctx context.Context, root *os.File) error {
	var report struct {
		Filesystems []mountRecord `json:"filesystems"`
	}
	cmd := exec.CommandContext(ctx, "findmnt", "--json", "--uniq", "--target", c.Mount, "--output", "TARGET,SOURCE,FSTYPE,OPTIONS,ID")
	output := &boundedBuffer{limit: 64 << 10}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return errors.New("cannot inspect visible media mount")
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil || len(report.Filesystems) != 1 {
		return errors.New("invalid media mount report")
	}
	id, err := mountID(root)
	if err != nil {
		return err
	}
	if err := validateMount(c, report.Filesystems[0], id); err != nil {
		return err
	}
	return unix.Faccessat(int(root.Fd()), ".", unix.W_OK, unix.AT_EACCESS)
}

// Walk through descriptors with no symlinks or nested mounts, including stale
// destination entries that rsync might remove. Administrative writers must stay quiescent.
func walkPinned(root *os.File, visit func(*os.File, string, bool) error) error {
	fd, err := unix.Openat(int(root.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(fd), root.Name())
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		fd, err := unix.Openat2(int(dir.Fd()), entry.Name(), &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
		if err != nil {
			return err
		}
		child := os.NewFile(uintptr(fd), entry.Name())
		info, err := child.Stat()
		if err == nil && !info.IsDir() {
			var st unix.Stat_t
			if err = unix.Fstat(fd, &st); err == nil && (!info.Mode().IsRegular() || st.Nlink != 1) {
				err = errors.New("unexpected filesystem entry or shared hard link")
			}
		}
		if err == nil && info.IsDir() {
			err = walkPinned(child, func(f *os.File, path string, isDir bool) error {
				return visit(f, filepath.Join(entry.Name(), path), isDir)
			})
		}
		if err == nil {
			err = visit(child, entry.Name(), info.IsDir())
		}
		err = errors.Join(err, child.Close())
		if err != nil {
			return err
		}
	}
	return visit(dir, ".", true)
}

func pinnedManifest(root *os.File) ([]FileEntry, error) {
	entries := []FileEntry{}
	err := walkPinned(root, func(f *os.File, path string, isDir bool) error {
		if isDir {
			return nil
		}
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if info.Size() == 0 || (filepath.Ext(path) != ".mp3" && filepath.Base(path) != "cover.jpg") {
			return errors.New("unexpected media file")
		}
		hash := sha256.New()
		n, err := io.Copy(hash, f)
		if err != nil {
			return err
		}
		if n != info.Size() {
			return errors.New("media changed while hashing")
		}
		entries = append(entries, FileEntry{Path: path, Size: n, SHA256: fmt.Sprintf("%x", hash.Sum(nil))})
		return nil
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, err
}

func verifyManifest(root *os.File, expected []FileEntry) error {
	actual, err := pinnedManifest(root)
	if err != nil {
		return err
	}
	sorted := append([]FileEntry(nil), expected...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	if len(actual) == 0 || !reflect.DeepEqual(actual, sorted) {
		return errors.New("media differs from frozen manifest")
	}
	return nil
}

// The caller verifies the source before creating any destination directories.
func copyVerifiedMedia(ctx context.Context, source, destination, lease *os.File, manifest []FileEntry) error {
	if err := walkPinned(destination, func(*os.File, string, bool) error { return nil }); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "rsync", "--recursive", "--checksum", "--delete-delay", "--no-links", "--no-devices", "--no-specials", "--one-file-system", "--perms", "--chmod=Du=rwx,Dgo=rx,Fu=rw,Fgo=r", "--fsync", "--", "/proc/self/fd/3/", "/proc/self/fd/4/")
	cmd.ExtraFiles = []*os.File{source, destination}
	if err := runMediaProcess(ctx, cmd, lease); err != nil {
		return fmt.Errorf("rsync failed: %w", err)
	}
	if err := verifyManifest(destination, manifest); err != nil {
		return err
	}
	return walkPinned(destination, func(f *os.File, _ string, _ bool) error { return f.Sync() })
}

func transfer(ctx context.Context, c StorageConfig, spool string, j Job, r Review, lease *os.File) error {
	if j.Phase != Copying || (r.Category != "Owned" && r.Category != "Loaned") {
		return errors.New("copy has no approved claim")
	}
	if _, err := storedDisc(j); err != nil {
		return err
	}
	attempt, err := openDirectory(filepath.Join(spool, j.ID))
	if err != nil {
		return err
	}
	defer attempt.Close()
	source, err := childDirectory(attempt, "media", false)
	if err != nil {
		return err
	}
	defer source.Close()
	// Validate the source before even creating a destination directory.
	if err := verifyManifest(source, r.Manifest); err != nil {
		return err
	}
	root, err := c.pin(ctx)
	if err != nil {
		return err
	}
	defer root.Close()
	current := root
	for _, part := range []string{"Music", "incoming", r.Category, j.DiscKey} {
		if err := c.check(ctx, root); err != nil {
			return err
		}
		next, err := childDirectory(current, part, true)
		if err != nil {
			return err
		}
		defer next.Close()
		current = next
	}
	if err := c.check(ctx, root); err != nil {
		return err
	}
	return copyVerifiedMedia(ctx, source, current, lease, r.Manifest)
}

func cleanupAttempt(spool string, j Job) error {
	if j.Phase != Cleaning || !uuidPattern.MatchString(j.ID) || (j.Outcome != Published && j.Outcome != Abandoned) {
		return errors.New("cleanup has no exclusive terminal claim")
	}
	root, err := openDirectory(spool)
	if err != nil {
		return err
	}
	defer root.Close()
	attempt, err := childDirectory(root, j.ID, false)
	if errors.Is(err, unix.ENOENT) {
		return root.Sync()
	}
	if err != nil {
		return err
	}
	defer attempt.Close()
	if err := walkPinned(attempt, func(*os.File, string, bool) error { return nil }); err != nil {
		return err
	}
	// Go's Root anchors RemoveAll at the validated spool descriptor, never at a newly resolved mountpoint.
	anchored, err := os.OpenRoot(fmt.Sprintf("/proc/self/fd/%d", root.Fd()))
	if err != nil {
		return err
	}
	defer anchored.Close()
	if err := anchored.RemoveAll(j.ID); err != nil {
		return err
	}
	return root.Sync()
}
