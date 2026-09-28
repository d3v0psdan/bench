package binman

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ulikunitz/xz"
)

// extract unpacks a verified download into dest. Only regular files,
// directories and in-tree links are materialized (no devices, no links
// pointing outside dest), and entry paths must stay inside dest.
func extract(d Download, src, dest string) error {
	root, err := filepath.EvalSymlinks(dest)
	if err != nil {
		return err
	}
	x := &extractor{dest: filepath.Clean(dest), root: root, strip: d.Strip}
	switch d.Archive {
	case "zip":
		err = x.zip(src)
	case "tar.gz":
		err = x.tarFile(src, func(r io.Reader) (io.Reader, error) { return gzip.NewReader(r) })
	case "tar.xz":
		err = x.tarFile(src, func(r io.Reader) (io.Reader, error) { return xz.NewReader(r) })
	case "raw":
		err = x.raw(src, d.File)
	default:
		return fmt.Errorf("unknown archive format %q", d.Archive)
	}
	if err != nil {
		return err
	}
	return x.finishLinks()
}

// extractor carries per-archive state: links are created only after every
// regular file exists, so the copy fallback (and target checks) see the
// final tree regardless of archive entry order.
type extractor struct {
	dest  string
	root  string // dest with symlinks resolved
	strip int
	links []link
}

type link struct {
	path   string // where the link goes
	target string // resolved absolute target inside dest
	raw    string // target as written in the archive (symlinks only)
	hard   bool
}

// entryPath maps an archive entry name to its location under dest after
// stripping leading components. ok=false means the entry is (part of) a
// stripped prefix and is skipped.
func (x *extractor) entryPath(name string) (p string, ok bool, err error) {
	parts := strings.Split(strings.Trim(path.Clean("/"+filepath.ToSlash(name)), "/"), "/")
	if len(parts) <= x.strip || (len(parts) == 1 && parts[0] == "") {
		return "", false, nil
	}
	rel := strings.Join(parts[x.strip:], "/")
	// Check the raw name too: path.Clean("/"+name) already neutralizes
	// "../", but a traversal entry means a hostile archive, so refuse it.
	for _, seg := range strings.Split(filepath.ToSlash(name), "/") {
		if seg == ".." {
			return "", false, fmt.Errorf("archive entry escapes destination: %q", name)
		}
	}
	p, err = x.inside(filepath.Join(x.dest, filepath.FromSlash(rel)))
	return p, err == nil, err
}

// inside rejects any path that isn't dest or below it (zip-slip).
func (x *extractor) inside(p string) (string, error) {
	p = filepath.Clean(p)
	if p != x.dest && !strings.HasPrefix(p, x.dest+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry escapes destination: %q", p)
	}
	return p, nil
}

// addSymlink records a symlink whose target must resolve inside dest.
// Absolute targets are refused outright.
func (x *extractor) addSymlink(p, target string) error {
	// Rooted forms count as absolute too: on Windows "\evil" and "C:evil"
	// aren't IsAbs, yet they resolve against the drive, not the link.
	if target == "" || filepath.IsAbs(target) || filepath.VolumeName(target) != "" ||
		strings.HasPrefix(target, "/") || strings.HasPrefix(target, `\`) {
		return fmt.Errorf("archive symlink %q has absolute or empty target %q", p, target)
	}
	resolved, err := x.inside(filepath.Join(filepath.Dir(p), filepath.FromSlash(target)))
	if err != nil {
		return fmt.Errorf("archive symlink %q points outside the install: %w", p, err)
	}
	x.links = append(x.links, link{path: p, target: resolved, raw: filepath.FromSlash(target)})
	return nil
}

// finishLinks materializes links. Symlinks stay symlinks where the OS
// allows it (vendor tarballs rely on e.g. libpq.so.5 → libpq.so.5.18);
// where it doesn't (Windows without developer mode) and for hard links,
// the target file is copied instead.
//
// The lexical check in addSymlink isn't enough once links exist: a link
// placed under an earlier directory link ("sub" -> ".", then "sub/x" ->
// "../evil") resolves differently on disk than on paper. So each link's
// real parent directory is resolved, and the target re-checked against it.
func (x *extractor) finishLinks() error {
	if len(x.links) == 0 {
		return nil
	}
	root, err := filepath.EvalSymlinks(x.dest)
	if err != nil {
		return err
	}
	for _, l := range x.links {
		if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
			return err
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(l.path))
		if err != nil {
			return err
		}
		if !within(root, parent) {
			return fmt.Errorf("archive link %q sits under a link that leaves the install", l.path)
		}
		if !l.hard && !within(root, filepath.Join(parent, l.raw)) {
			return fmt.Errorf("archive symlink %q points outside the install", l.path)
		}
		_ = os.Remove(l.path) // a later entry replaces an earlier one, like tar
		if !l.hard {
			if err := os.Symlink(l.raw, l.path); err == nil {
				// The target may itself walk through links ("x/../.."):
				// only the fully resolved path tells where it really goes.
				if resolved, err := filepath.EvalSymlinks(l.path); err == nil && !within(root, resolved) {
					_ = os.Remove(l.path)
					return fmt.Errorf("archive symlink %q resolves outside the install", l.path)
				}
				continue
			}
		}
		if err := copyFile(l.target, l.path); err != nil {
			return fmt.Errorf("materializing link %s: %w", l.path, err)
		}
	}
	return nil
}

// within reports whether p is root or below it (both already resolved).
func within(root, p string) bool {
	p = filepath.Clean(p)
	return p == root || strings.HasPrefix(p, root+string(os.PathSeparator))
}

func copyFile(src, dst string) error {
	fi, err := os.Stat(src) // follows symlinks: chains resolve to the file
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("link target %s is not a regular file", src)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return writeEntry(dst, fi.Mode(), in)
}

func writeEntry(path string, mode os.FileMode, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	perm := mode.Perm() | 0o600 // always writable/readable by owner
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// guardParent refuses to write p when its nearest existing ancestor
// resolves outside the install: an earlier download's links may sit on
// the path (all downloads of a build extract into one directory).
func (x *extractor) guardParent(p string) error {
	dir := filepath.Dir(p)
	for {
		if _, err := os.Lstat(dir); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	if !within(x.root, resolved) {
		return fmt.Errorf("archive entry %q would be written through a link that leaves the install", p)
	}
	return nil
}

// raw installs a bare executable download (e.g. Meilisearch) as name.
func (x *extractor) raw(src, name string) error {
	if name == "" || filepath.Base(name) != name {
		return fmt.Errorf("raw download needs a plain file name, got %q", name)
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeEntry(filepath.Join(x.dest, name), 0o755, f)
}

func (x *extractor) zip(src string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		// Go's reader flags traversal names (ErrInsecurePath) but still
		// returns a usable reader: close it and refuse the archive.
		if r != nil {
			r.Close()
		}
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		p, ok, err := x.entryPath(f.Name)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		mode := f.FileInfo().Mode()
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case mode&os.ModeSymlink != 0:
			target, err := readZipLink(f)
			if err != nil {
				return err
			}
			if err := x.addSymlink(p, target); err != nil {
				return err
			}
		case mode.IsRegular():
			rc, err := f.Open()
			if err != nil {
				return err
			}
			if err = x.guardParent(p); err == nil {
				err = writeEntry(p, mode, rc)
			}
			rc.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func readZipLink(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, 4096))
	return string(b), err
}

func (x *extractor) tarFile(src string, decompress func(io.Reader) (io.Reader, error)) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	r, err := decompress(f)
	if err != nil {
		return err
	}
	if c, ok := r.(io.Closer); ok {
		defer c.Close()
	}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		p, ok, err := x.entryPath(hdr.Name)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := x.guardParent(p); err != nil {
				return err
			}
			if err := writeEntry(p, os.FileMode(hdr.Mode), tr); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := x.addSymlink(p, hdr.Linkname); err != nil {
				return err
			}
		case tar.TypeLink:
			// Hard link names are archive-relative, like entry names.
			target, ok, err := x.entryPath(hdr.Linkname)
			if err != nil || !ok {
				return fmt.Errorf("archive hard link %q has bad target %q", hdr.Name, hdr.Linkname)
			}
			x.links = append(x.links, link{path: p, target: target, hard: true})
		}
	}
}
