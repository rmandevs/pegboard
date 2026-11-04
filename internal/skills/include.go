package skills

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// StackFile is the file at the root of an included stack.
const StackFile = "pegboard.yaml"

// IncludeKind is where an included stack comes from (SPEC §3.5).
type IncludeKind string

const (
	IncludeLocal IncludeKind = "local"
	IncludeGit   IncludeKind = "git"
	IncludeZip   IncludeKind = "zip"
)

// Include is a parsed include source.
type Include struct {
	Raw  string
	Kind IncludeKind
	// Git: the same fields as a skill source. Local and local zips: Dir.
	Git Source
	Dir string
	URL string // zip over HTTPS
}

// IsLocalPath reports whether s is written as a path on disk.
func IsLocalPath(s string) bool {
	return strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") || s == "." || s == ".." ||
		strings.HasPrefix(s, "~/") || filepath.IsAbs(s) || strings.HasPrefix(s, "/")
}

// ParseInclude interprets an include source: a local directory, a git repo
// (the same conventions as skills, without @skill), or a zip.
func ParseInclude(raw string) (Include, error) {
	in := Include{Raw: raw}
	s := strings.TrimSpace(raw)
	if s == "" {
		return in, fmt.Errorf("empty include")
	}
	if strings.HasSuffix(strings.ToLower(strings.SplitN(s, "?", 2)[0]), ".zip") {
		in.Kind = IncludeZip
		switch {
		case strings.HasPrefix(s, "https://"):
			in.URL = s
		case strings.HasPrefix(s, "http://"):
			return in, fmt.Errorf("%s: zips are only downloaded over https", raw)
		case IsLocalPath(s):
			in.Dir = s
		default:
			return in, fmt.Errorf("%s: a zip is a local path (./x.zip) or an https:// URL", raw)
		}
		return in, nil
	}
	if IsLocalPath(s) {
		in.Kind, in.Dir = IncludeLocal, s
		return in, nil
	}
	src, err := Parse(s)
	if err != nil {
		return in, fmt.Errorf("include %w", err)
	}
	if src.Kind != Git {
		return in, fmt.Errorf("%s: not a stack source", raw)
	}
	if src.Skill != "" {
		return in, fmt.Errorf("%s: owner/repo@skill does not apply to include; use owner/repo//path", raw)
	}
	in.Kind, in.Git = IncludeGit, src
	return in, nil
}

// Key identifies the repository and path of a git include regardless of its
// ref: the same key with two refs is an error.
func (in Include) Key() string {
	if in.Kind == IncludeGit {
		return in.Git.URL + "//" + in.Git.Path
	}
	return in.Raw
}

// Ref is the ref of a git include ("" = default branch).
func (in Include) Ref() string { return in.Git.Ref }

// LockedInclude pins an included stack: its commit (git), the hash of its
// content, and the MCPs that were approved from it.
type LockedInclude struct {
	Commit string `json:"commit,omitempty"`
	Hash   string `json:"hash"`
	// MCPs records each approved MCP's fingerprint (its command or URL,
	// args, and variable names): a new or changed MCP needs --approve.
	MCPs map[string]string `json:"mcps,omitempty"`
}

// Fetched is an include on disk.
type Fetched struct {
	Dir    string // directory with pegboard.yaml
	Commit string
	Hash   string
}

// HTTPGet downloads zips; tests replace it.
var HTTPGet = func(url string) ([]byte, error) {
	c := &http.Client{Timeout: 60 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxZip+1))
}

// Limits for zips: a stack is configuration, not a distribution.
const (
	maxZip      = 64 << 20
	maxUnpacked = 256 << 20
	maxEntries  = 10000
)

func (r *Resolver) stacksDir(name string) string { return filepath.Join(r.Cache, "stacks", name) }

// FetchInclude places a remote include in the cache and pins it in the
// lock. With the lock and the cache complete, it needs no network.
func (r *Resolver) FetchInclude(in Include, base string) (Fetched, error) {
	switch in.Kind {
	case IncludeGit:
		return r.fetchGitInclude(in)
	case IncludeZip:
		return r.fetchZipInclude(in, base)
	}
	return Fetched{}, fmt.Errorf("%s: not a remote include", in.Raw)
}

func (r *Resolver) pinned(raw string) (LockedInclude, bool) {
	if r.Lock == nil || r.Update {
		return LockedInclude{}, false
	}
	l, ok := r.Lock.Includes[raw]
	return l, ok && l.Hash != ""
}

func (r *Resolver) fetchGitInclude(in Include) (Fetched, error) {
	pin, pinned := r.pinned(in.Raw)
	if pinned {
		dir := r.stacksDir(strings.TrimPrefix(pin.Hash, "sha256:"))
		if exists(filepath.Join(dir, StackFile)) {
			return Fetched{Dir: dir, Commit: pin.Commit, Hash: pin.Hash}, nil
		}
	}
	ref := in.Git.Ref
	if pinned && pin.Commit != "" {
		ref = pin.Commit
	}
	if ref == "" {
		ref = "HEAD"
	}
	tmp, err := r.tmp()
	if err != nil {
		return Fetched{}, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	for _, args := range [][]string{
		{"init", "-q"},
		{"fetch", "-q", "--depth", "1", in.Git.URL, ref},
		{"checkout", "-q", "FETCH_HEAD"},
	} {
		if _, err := r.git(tmp, args...); err != nil {
			return Fetched{}, fmt.Errorf("include %s: %w", in.Raw, err)
		}
	}
	commit, err := r.git(tmp, "rev-parse", "HEAD")
	if err != nil {
		return Fetched{}, err
	}
	root := filepath.Join(tmp, filepath.FromSlash(in.Git.Path))
	if !exists(filepath.Join(root, StackFile)) {
		return Fetched{}, fmt.Errorf("include %s: no %s at the root of the stack", in.Raw, StackFile)
	}
	f, err := r.storeStack(root)
	if err != nil {
		return Fetched{}, err
	}
	f.Commit = commit
	if pinned && pin.Commit == commit && pin.Hash != f.Hash {
		return Fetched{}, fmt.Errorf("include %s: content does not match %s (commit %s); check with `pegboard update`",
			in.Raw, LockFile, Short(commit))
	}
	r.lockInclude(in.Raw, f)
	return f, nil
}

func (r *Resolver) lockInclude(raw string, f Fetched) {
	if r.Lock == nil {
		return
	}
	old := r.Lock.Includes[raw]
	if old.Commit == f.Commit && old.Hash == f.Hash {
		return
	}
	nl := LockedInclude{Commit: f.Commit, Hash: f.Hash, MCPs: old.MCPs}
	r.Lock.setInclude(raw, nl)
}

// storeStack copies a stack directory into the cache by content hash.
func (r *Resolver) storeStack(dir string) (Fetched, error) {
	h, err := Hash(dir)
	if err != nil {
		return Fetched{}, err
	}
	dst := r.stacksDir(strings.TrimPrefix(h, "sha256:"))
	if !exists(dst) {
		tmp, err := r.tmp()
		if err != nil {
			return Fetched{}, err
		}
		staged := filepath.Join(tmp, "stack")
		if err := CopyDir(dir, staged); err != nil {
			_ = os.RemoveAll(tmp)
			return Fetched{}, err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			_ = os.RemoveAll(tmp)
			return Fetched{}, err
		}
		if err := os.Rename(staged, dst); err != nil && !exists(dst) {
			_ = os.RemoveAll(tmp)
			return Fetched{}, err
		}
		_ = os.RemoveAll(tmp)
	}
	return Fetched{Dir: dst, Hash: h}, nil
}

func (r *Resolver) fetchZipInclude(in Include, base string) (Fetched, error) {
	pin, pinned := r.pinned(in.Raw)
	if pinned {
		dir := r.stacksDir("zip-" + strings.TrimPrefix(pin.Hash, "sha256:"))
		if root, err := zipRoot(dir); err == nil {
			return Fetched{Dir: root, Hash: pin.Hash}, nil
		}
	}
	var data []byte
	var err error
	if in.URL != "" {
		data, err = HTTPGet(in.URL)
	} else {
		p := in.Dir
		switch {
		case strings.HasPrefix(p, "~/"):
			p = filepath.Join(r.Home, p[2:])
		case !filepath.IsAbs(p):
			p = filepath.Join(base, p)
		}
		data, err = os.ReadFile(p)
	}
	if err != nil {
		return Fetched{}, fmt.Errorf("include %s: %w", in.Raw, err)
	}
	if len(data) > maxZip {
		return Fetched{}, fmt.Errorf("include %s: the zip exceeds %d MiB", in.Raw, maxZip>>20)
	}
	sum := sha256.Sum256(data)
	h := "sha256:" + hex.EncodeToString(sum[:])
	if pinned && pin.Hash != h {
		return Fetched{}, fmt.Errorf("include %s: the zip does not match %s; check with `pegboard update`", in.Raw, LockFile)
	}
	dst := r.stacksDir("zip-" + strings.TrimPrefix(h, "sha256:"))
	if !exists(dst) {
		tmp, err := r.tmp()
		if err != nil {
			return Fetched{}, err
		}
		staged := filepath.Join(tmp, "zip")
		if err := Unzip(data, staged); err != nil {
			_ = os.RemoveAll(tmp)
			return Fetched{}, fmt.Errorf("include %s: %w", in.Raw, err)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			_ = os.RemoveAll(tmp)
			return Fetched{}, err
		}
		if err := os.Rename(staged, dst); err != nil && !exists(dst) {
			_ = os.RemoveAll(tmp)
			return Fetched{}, err
		}
		_ = os.RemoveAll(tmp)
	}
	root, err := zipRoot(dst)
	if err != nil {
		return Fetched{}, fmt.Errorf("include %s: %w", in.Raw, err)
	}
	f := Fetched{Dir: root, Hash: h}
	r.lockInclude(in.Raw, f)
	return f, nil
}

// escapes reports whether a zip entry name could land outside the
// destination: absolute, with a drive, or with a .. segment.
func escapes(name string) bool {
	if strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return true
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// zipRoot is where the stack is inside an unpacked zip: at the root or
// inside its single top-level directory.
func zipRoot(dir string) (string, error) {
	if exists(filepath.Join(dir, StackFile)) {
		return dir, nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	if len(ents) == 1 && ents[0].IsDir() && exists(filepath.Join(dir, ents[0].Name(), StackFile)) {
		return filepath.Join(dir, ents[0].Name()), nil
	}
	return "", fmt.Errorf("no %s at the root of the zip or of its single top-level directory", StackFile)
}

// Unzip unpacks a zip into dst. An entry that escapes dst, is a symlink, or
// exceeds the limits makes the whole zip fail.
func Unzip(data []byte, dst string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	if len(zr.File) > maxEntries {
		return fmt.Errorf("the zip has more than %d entries", maxEntries)
	}
	var total int64
	root := filepath.Clean(dst) + string(os.PathSeparator)
	for _, f := range zr.File {
		// Reject any name with "..", a drive, or a leading slash outright
		// (also names like "a..b": a stack has no reason to use them), then
		// check the joined path is still under dst.
		if strings.Contains(f.Name, "..") || escapes(strings.ReplaceAll(f.Name, "\\", "/")) {
			return fmt.Errorf("entry %q escapes the destination", f.Name)
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("entry %q is a symlink", f.Name)
		}
		target := filepath.Join(dst, filepath.FromSlash(strings.ReplaceAll(f.Name, "\\", "/")))
		if !strings.HasPrefix(target+string(os.PathSeparator), root) {
			return fmt.Errorf("entry %q escapes the destination", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		total += int64(f.UncompressedSize64)
		if total > maxUnpacked {
			return fmt.Errorf("the zip unpacks to more than %d MiB", maxUnpacked>>20)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if f.Mode()&0o111 != 0 {
			mode = 0o755
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			_ = rc.Close()
			return err
		}
		n, err := io.Copy(out, io.LimitReader(rc, maxUnpacked-total+int64(f.UncompressedSize64)+1))
		_ = rc.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		if n > int64(f.UncompressedSize64) {
			return fmt.Errorf("entry %q is larger than it declares", f.Name)
		}
	}
	return nil
}
