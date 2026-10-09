// Package download fetches archives, verifies their checksum and unpacks them safely.
package download

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/md5" //nolint:gosec // MySQL only publishes MD5; used as an integrity check, not security.
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Algo is a checksum algorithm.
type Algo string

const (
	SHA256 Algo = "sha256"
	MD5    Algo = "md5"
)

// File downloads url to dest and verifies it against the expected hex checksum.
// An existing file with a matching checksum is reused. Progress, if set, is
// called with bytes written and total (-1 when unknown).
func File(ctx context.Context, url, dest string, algo Algo, want string, progress func(done, total int64)) error {
	if sum, err := sumFile(dest, algo); err == nil && strings.EqualFold(sum, want) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s failed: %w. Check your internet connection and retry", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s failed: HTTP %d. The version may have been removed; update the Hyper Stack manifest", url, resp.StatusCode)
	}

	tmp := dest + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := newHash(algo)
	w := io.MultiWriter(out, h)
	var done int64
	buf := make([]byte, 256*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				out.Close()
				return werr
			}
			done += int64(n)
			if progress != nil {
				progress(done, resp.ContentLength)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			return fmt.Errorf("download interrupted: %w. Retry; partial data is discarded", rerr)
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
		_ = os.Remove(tmp)
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s. The file was not installed", url, got, want)
	}
	return os.Rename(tmp, dest)
}

// Unpack extracts a .zip or .tar.gz archive into dir. stripTop removes a single
// leading directory component from every entry (like tar --strip-components=1).
func Unpack(archive, dir string, stripTop bool) error {
	switch {
	case strings.HasSuffix(archive, ".zip"):
		return unzip(archive, dir, stripTop)
	case strings.HasSuffix(archive, ".tar.gz"), strings.HasSuffix(archive, ".tgz"):
		return untar(archive, dir, stripTop)
	}
	return fmt.Errorf("unsupported archive type: %s", archive)
}

func unzip(archive, dir string, strip bool) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("cannot open %s: %w", archive, err)
	}
	defer r.Close()
	for _, f := range r.File {
		target, ok, err := safeTarget(dir, f.Name, strip)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeFile(target, rc, f.Mode())
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func untar(archive, dir string, strip bool) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", archive, err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target, ok, err := safeTarget(dir, h.Name, strip)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		switch h.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			err = writeFile(target, tr, h.FileInfo().Mode())
		case tar.TypeSymlink:
			// Reject links that escape the destination.
			if _, _, e := safeTarget(dir, filepath.ToSlash(filepath.Join(filepath.Dir(h.Name), h.Linkname)), strip); e != nil { //nolint:gosec // safeTarget rejects any entry that escapes the destination
				return e
			}
			if err = os.MkdirAll(filepath.Dir(target), 0o755); err == nil {
				_ = os.Remove(target)
				err = os.Symlink(h.Linkname, target)
			}
		}
		if err != nil {
			return err
		}
	}
}

// safeTarget maps an archive entry name to a path inside dir, rejecting
// traversal. ok is false for entries that vanish after stripping.
func safeTarget(dir, name string, strip bool) (string, bool, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if strip {
		i := strings.Index(name, "/")
		if i < 0 {
			return "", false, nil
		}
		name = name[i+1:]
	}
	name = strings.TrimPrefix(name, "./")
	if name == "" {
		return "", false, nil
	}
	target := filepath.Join(dir, filepath.FromSlash(name))
	rel, err := filepath.Rel(dir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(filepath.FromSlash(name)) {
		return "", false, fmt.Errorf("archive entry %q escapes the install folder; refusing to unpack", name)
	}
	return target, true, nil
}

func writeFile(path string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode|0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil { //nolint:gosec // size bounded by checksummed archive
		out.Close()
		return err
	}
	return out.Close()
}

func newHash(a Algo) hash.Hash {
	if a == MD5 {
		return md5.New() //nolint:gosec
	}
	return sha256.New()
}

func sumFile(path string, a Algo) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := newHash(a)
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
