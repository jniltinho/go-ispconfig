// Package acme issues and renews Let's Encrypt certificates in process,
// replacing the shell-out to acme.sh or certbot (spec acme-issuance).
//
// Certificates are stored in certbot's layout, because that is what an adopted
// install already has on disk and what monitoring, third-party scripts and
// every Let's Encrypt tutorial expect (design D5):
//
//	/etc/letsencrypt/live/<lineage>/{cert,chain,fullchain,privkey}.pem
//	  → symlinks into ../../archive/<lineage>/
//	/etc/letsencrypt/archive/<lineage>/{cert,chain,fullchain,privkey}<N>.pem
//	/etc/letsencrypt/renewal/<lineage>.conf
//
// The renewal file is not decoration: ISPConfig does not glob live/, it walks
// the renewal directory and reads the four paths out of each .conf
// (letsencrypt.inc.php:614), so writing them is what lets a legacy panel find
// certificates this port issued.
package acme

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// DefaultRoot is certbot's tree. Overridable for tests only.
const DefaultRoot = "/etc/letsencrypt"

// pemNames are the four files certbot writes per generation, in the order the
// renewal config lists them.
var pemNames = [...]string{"cert", "chain", "fullchain", "privkey"}

// lineageRe guards the directory component built from a domain name: it is
// interpolated into paths, so anything outside this set is refused rather than
// escaped.
var lineageRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// Store is the certbot-shaped certificate tree rooted at Root.
type Store struct {
	Root string
}

// NewStore returns a Store over root, defaulting to certbot's location.
func NewStore(root string) *Store {
	if root == "" {
		root = DefaultRoot
	}
	return &Store{Root: root}
}

// Lineage is certbot's name for one certificate's history. ECDSA gets the
// _ecc suffix the legacy uses (installer_base.lib.php:3454), so an adopted
// host that already has both keeps them apart instead of gaining a third,
// competing lineage.
func Lineage(mainDomain, keyType string) (string, error) {
	name := strings.TrimPrefix(mainDomain, "*.")
	if strings.EqualFold(keyType, "ecdsa") || strings.EqualFold(keyType, "ec") {
		name += "_ecc"
	}
	if !lineageRe.MatchString(name) {
		return "", fmt.Errorf("acme: refusing unsafe lineage name %q", mainDomain)
	}
	return name, nil
}

func (s *Store) liveDir(l string) string    { return filepath.Join(s.Root, "live", l) }
func (s *Store) archiveDir(l string) string { return filepath.Join(s.Root, "archive", l) }
func (s *Store) renewalFile(l string) string {
	return filepath.Join(s.Root, "renewal", l+".conf")
}

// LivePaths returns the four stable paths callers reference. They are the
// symlinks, so a reader that opens one never observes a half-written file.
func (s *Store) LivePaths(lineage string) (cert, chain, fullchain, privkey string) {
	d := s.liveDir(lineage)
	return filepath.Join(d, "cert.pem"), filepath.Join(d, "chain.pem"),
		filepath.Join(d, "fullchain.pem"), filepath.Join(d, "privkey.pem")
}

// nextGeneration returns the number the next write takes: one above the
// highest already in archive/, or 1 for a first issuance.
func (s *Store) nextGeneration(lineage string) (int, error) {
	entries, err := os.ReadDir(s.archiveDir(lineage))
	if os.IsNotExist(err) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	highest := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "cert") || !strings.HasSuffix(name, ".pem") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "cert"), ".pem"))
		if err == nil && n > highest {
			highest = n
		}
	}
	return highest + 1, nil
}

// Material is one issued certificate, split the way certbot stores it.
type Material struct {
	Cert      []byte // leaf only
	Chain     []byte // intermediates only
	Fullchain []byte // leaf + intermediates
	Privkey   []byte
}

// Save writes the next generation into archive/, repoints the four live/
// symlinks at it and writes the renewal config. The symlink swap is what makes
// a renewal atomic for readers: the PEMs are complete on disk before any
// pointer moves.
func (s *Store) Save(lineage string, m Material, domains []string, caURL string) error {
	if !lineageRe.MatchString(lineage) {
		return fmt.Errorf("acme: refusing unsafe lineage name %q", lineage)
	}
	gen, err := s.nextGeneration(lineage)
	if err != nil {
		return fmt.Errorf("acme: reading archive generations: %w", err)
	}
	arch := s.archiveDir(lineage)
	if err := os.MkdirAll(arch, 0o755); err != nil {
		return err
	}
	blobs := map[string][]byte{
		"cert": m.Cert, "chain": m.Chain, "fullchain": m.Fullchain, "privkey": m.Privkey,
	}
	for _, n := range pemNames {
		perm := os.FileMode(0o644)
		if n == "privkey" {
			perm = 0o600
		}
		path := filepath.Join(arch, fmt.Sprintf("%s%d.pem", n, gen))
		if err := writeFileAtomic(path, blobs[n], perm); err != nil {
			return err
		}
	}

	live := s.liveDir(lineage)
	if err := os.MkdirAll(live, 0o755); err != nil {
		return err
	}
	for _, n := range pemNames {
		// Relative, as certbot writes them, so the tree survives being moved
		// or bind-mounted somewhere else.
		target := filepath.Join("..", "..", "archive", lineage, fmt.Sprintf("%s%d.pem", n, gen))
		if err := replaceSymlink(target, filepath.Join(live, n+".pem")); err != nil {
			return err
		}
	}
	return s.writeRenewal(lineage, domains, caURL)
}

// writeRenewal emits the discovery contract: the four absolute paths, in the
// key = value form get_certificate_list parses, terminated before the
// [[webroot_map]] marker it stops at.
func (s *Store) writeRenewal(lineage string, domains []string, caURL string) error {
	cert, chain, fullchain, privkey := s.LivePaths(lineage)
	var b strings.Builder
	fmt.Fprintf(&b, "# Managed by go-ispconfig. Do not edit by hand.\n")
	fmt.Fprintf(&b, "version = 4.0.0\n")
	fmt.Fprintf(&b, "archive_dir = %s\n", s.archiveDir(lineage))
	fmt.Fprintf(&b, "cert = %s\n", cert)
	fmt.Fprintf(&b, "privkey = %s\n", privkey)
	fmt.Fprintf(&b, "chain = %s\n", chain)
	fmt.Fprintf(&b, "fullchain = %s\n", fullchain)
	fmt.Fprintf(&b, "\n[renewalparams]\n")
	fmt.Fprintf(&b, "authenticator = webroot\n")
	if caURL != "" {
		fmt.Fprintf(&b, "server = %s\n", caURL)
	}
	sorted := append([]string(nil), domains...)
	sort.Strings(sorted)
	fmt.Fprintf(&b, "domains = %s\n", strings.Join(sorted, ", "))
	if err := os.MkdirAll(filepath.Dir(s.renewalFile(lineage)), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(s.renewalFile(lineage), []byte(b.String()), 0o644)
}

// writeFileAtomic writes through a temp file in the same directory so a reader
// never sees a partial PEM, and a crash leaves either the old file or none.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		// Both are cleanup after a successful rename has already moved the
		// file, or after an error the caller is being told about.
		_ = tmp.Close()
		_ = os.Remove(name)
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// replaceSymlink points link at target, replacing whatever is there. Symlink
// then rename, because os.Symlink refuses an existing name and unlinking first
// would leave a window with no certificate at all.
func replaceSymlink(target, link string) error {
	tmp := link + ".tmp"
	_ = os.Remove(tmp) // a leftover from an interrupted run
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
