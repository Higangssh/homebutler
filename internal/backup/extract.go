package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// extractTarGz extracts the gzipped tar archive at archivePath into dest.
//
// homebutler used to shell out to `tar xzf -C <dest>` for this. That was safe,
// but only because both GNU tar and bsdtar decline the dangerous member names
// on their own: a leading "/" is stripped, and a member containing ".." is
// skipped with a warning. Member names come from whoever built the archive,
// exactly as manifest paths do, so the destination held because of a default in
// a program homebutler invokes rather than because of anything in this
// repository. Nothing here asserted it and no test covered it, so a host with a
// different tar — or a later switch to an archive library — would have lost the
// property with no signal at all.
//
// Every member is checked here now, and a member that asks to be written
// outside dest fails the extraction rather than being skipped. Skipping is what
// tar does, and it means an archive that tried to escape still reports success
// while having restored less than it said it would.
func extractTarGz(archivePath, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive %s: %w", archivePath, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("read archive %s: %w", archivePath, err)
	}
	defer gz.Close()

	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	// Resolved once: dest is created above, so it exists, and every member is
	// compared against where it really is rather than how it was spelled.
	destReal, err := filepath.EvalSymlinks(dest)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", dest, err)
	}

	// Directory modes are applied after everything is written. A directory
	// stored read-only would otherwise stop its own contents from being
	// extracted into it, which is why tar makes the same second pass.
	type deferredDir struct {
		path    string
		mode    os.FileMode
		modTime time.Time
		uid     int
		gid     int
	}
	var dirs []deferredDir

	// tar restores ownership when it runs as root and cannot when it does not.
	// `sudo homebutler restore` is documented for exactly the case where the
	// target is not writable by the invoking user, so a restore that silently
	// dropped ownership would break the setups most likely to need it.
	restoreOwner := os.Geteuid() == 0

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read archive %s: %w", archivePath, err)
		}

		target, err := memberTarget(hdr.Name, destReal)
		if err != nil {
			return fmt.Errorf("archive %s: %w", archivePath, err)
		}
		if target == destReal {
			continue // the "./" entry tar writes for the root of the tree
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create directory %s: %w", target, err)
			}
			dirs = append(dirs, deferredDir{target, hdr.FileInfo().Mode().Perm(), hdr.ModTime, hdr.Uid, hdr.Gid})
		case tar.TypeReg:
			body, skip, err := memberBody(tr, filepath.Base(target))
			if err != nil {
				return fmt.Errorf("archive %s: read %q: %w", archivePath, hdr.Name, err)
			}
			if skip {
				continue
			}
			if err := writeMember(body, target, hdr.FileInfo().Mode().Perm()); err != nil {
				return err
			}
			if err := applyMetadata(target, hdr.ModTime, hdr.Uid, hdr.Gid, restoreOwner); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// The link target is not checked, because a backup can legitimately
			// contain a symlink to an absolute path outside the tree. It is
			// safe to create because it is never followed: a later member whose
			// destination goes through it is refused by memberTarget, which
			// resolves before it compares.
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create directory for %s: %w", target, err)
			}
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("replace %s: %w", target, err)
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("create symlink %s: %w", target, err)
			}
			if restoreOwner {
				if err := os.Lchown(target, hdr.Uid, hdr.Gid); err != nil {
					return fmt.Errorf("set owner on %s: %w", target, err)
				}
			}
		case tar.TypeLink:
			// A hard link names a file that already exists. Left unchecked it
			// is a way to reach content outside the tree without ever naming a
			// path outside it.
			source, err := memberTarget(hdr.Linkname, destReal)
			if err != nil {
				return fmt.Errorf("archive %s: hard link %q: %w", archivePath, hdr.Name, err)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create directory for %s: %w", target, err)
			}
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("replace %s: %w", target, err)
			}
			if err := os.Link(source, target); err != nil {
				return fmt.Errorf("create hard link %s: %w", target, err)
			}
		default:
			// Device nodes, FIFOs and sockets. A volume backup has no reason to
			// carry one, creating them is privileged, and dropping them quietly
			// would mean a restore that says it succeeded while omitting part of
			// the archive. Naming what was found is more useful than either.
			return fmt.Errorf("archive %s: refusing member %q of unsupported type %q",
				archivePath, hdr.Name, string(hdr.Typeflag))
		}
	}

	// Deepest first, so a directory made read-only cannot block the one inside it.
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Chmod(dirs[i].path, dirs[i].mode); err != nil {
			return fmt.Errorf("set mode on %s: %w", dirs[i].path, err)
		}
		if err := applyMetadata(dirs[i].path, dirs[i].modTime, dirs[i].uid, dirs[i].gid, restoreOwner); err != nil {
			return err
		}
	}
	return nil
}

// applyMetadata restores the modification time, and the ownership when there is
// any point in trying. Both match what tar does by default.
func applyMetadata(target string, modTime time.Time, uid, gid int, restoreOwner bool) error {
	if restoreOwner {
		if err := os.Lchown(target, uid, gid); err != nil {
			return fmt.Errorf("set owner on %s: %w", target, err)
		}
	}
	if modTime.IsZero() {
		return nil
	}
	if err := os.Chtimes(target, modTime, modTime); err != nil {
		return fmt.Errorf("set times on %s: %w", target, err)
	}
	return nil
}

// memberTarget resolves where an archive member wants to be written and refuses
// it unless that is inside destReal.
//
// Two checks, and both are needed. The name is rejected if it is absolute or
// climbs out with "..", which is the case tar already declines. Then the parent
// directory is resolved, because an earlier member of the same archive can have
// been a symlink, and a name that is lexically inside the tree can still resolve
// out of it — the same weakness that #91 found in bind-mount containment.
func memberTarget(name, destReal string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("refusing member with an empty name")
	}
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("refusing member %q: absolute path", name)
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing member %q: climbs out of the archive root", name)
	}

	target := filepath.Join(destReal, clean)
	if target == destReal {
		// The "./" entry tar writes for the root of the tree. Nothing is
		// created for it, and it has no parent inside dest to resolve.
		return target, nil
	}
	if !strings.HasPrefix(target, destReal+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing member %q: resolves outside the destination", name)
	}

	// Whatever exists of the parent chain is resolved; what does not exist yet
	// is about to be created here and cannot be a link to somewhere else.
	parent := filepath.Dir(target)
	existing := parent
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		next := filepath.Dir(existing)
		if next == existing {
			break
		}
		existing = next
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", fmt.Errorf("refusing member %q: cannot resolve %s: %w", name, existing, err)
	}
	if real != destReal && !strings.HasPrefix(real, destReal+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing member %q: %s resolves to %s, outside the destination", name, existing, real)
	}
	return target, nil
}

// appleDoubleMagic opens every AppleDouble record.
var appleDoubleMagic = []byte{0x00, 0x05, 0x16, 0x07}

// memberBody returns the member's contents, and whether to skip it entirely.
//
// Archiving on macOS turns a file that carries extended attributes into two
// members: the file, and a sibling named "._<file>" holding the attributes.
// bsdtar consumes the second one on the way back out, so it never appears as a
// file — which is why homebutler never had to know about them while it was
// shelling out to tar on macOS. Written literally they are junk that was not in
// the source, and they land in restored volumes.
//
// The name alone is not enough to decide: a real file can be called "._notes".
// The AppleDouble magic is, so only a member that has it is dropped.
func memberBody(tr *tar.Reader, base string) (io.Reader, bool, error) {
	if !strings.HasPrefix(base, "._") {
		return tr, false, nil
	}
	head := make([]byte, len(appleDoubleMagic))
	n, err := io.ReadFull(tr, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, false, err
	}
	if n == len(appleDoubleMagic) && bytes.Equal(head, appleDoubleMagic) {
		return nil, true, nil
	}
	// Not an AppleDouble record after all, so put back what was read to look.
	return io.MultiReader(bytes.NewReader(head[:n]), tr), false, nil
}

// writeMember writes one regular file, creating its parent directories.
func writeMember(tr io.Reader, target string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", target, err)
	}
	// O_CREATE without O_EXCL overwrites, which is what a restore means to do,
	// but the existing entry is removed first so an existing symlink is
	// replaced rather than written through.
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("replace %s: %w", target, err)
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}
	if _, err := io.Copy(f, tr); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", target, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	return os.Chmod(target, mode)
}
