package apache2

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"go-ispconfig/internal/engine"
)

// ISPConfig delimits the block it owns inside a user-editable .htaccess so
// an operator's own rules survive a regeneration.
const (
	htaccessBegin = "### ISPConfig auth BEGIN ###"
	htaccessEnd   = "### ISPConfig auth END ###"
)

// folderAuth is one web_folder protected by HTTP basic auth, with its users.
type folderAuth struct {
	// path is the folder relative to the document root ("" is the root).
	path string
	// users are the (username, password) pairs; passwords are already
	// hashed when they arrive from the DB as crypt strings.
	users []folderUser
}

// folderUser is one row of web_folder_user.
type folderUser struct {
	name string
	// hash is the stored password. ISPConfig writes crypt/bcrypt hashes; a
	// value that is not recognisably hashed is bcrypt-hashed before writing.
	hash string
}

// writeFolderAuth writes the .htaccess/.htpasswd pair protecting one folder.
// Unlike nginx (which carries auth in the vhost), Apache reads them from the
// document root, so both files live inside the site and are owned by it.
func writeFolderAuth(docroot string, f folderAuth, chown func(path string) error) error {
	if err := safeSitePath(docroot, "/"); err != nil {
		return err
	}
	dir := docroot
	if f.path != "" {
		clean := filepath.Clean("/" + strings.Trim(f.path, "/"))
		dir = filepath.Join(docroot, clean)
		if err := safeSitePath(dir, docroot); err != nil {
			return err
		}
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("apache2: protected folder %q does not exist", dir)
	}

	passwdFile := filepath.Join(dir, ".htpasswd")
	var lines []string
	for _, u := range f.users {
		if u.name == "" || strings.ContainsAny(u.name, ":\n\r") {
			return fmt.Errorf("apache2: invalid .htpasswd user name %q", u.name)
		}
		hash, err := htpasswdHash(u.hash)
		if err != nil {
			return err
		}
		lines = append(lines, u.name+":"+hash)
	}
	if err := writeIfChanged(passwdFile, []byte(strings.Join(lines, "\n")+"\n"), 0o640); err != nil {
		return err
	}
	if chown != nil {
		if err := chown(passwdFile); err != nil {
			return err
		}
	}

	block := strings.Join([]string{
		htaccessBegin,
		"AuthType Basic",
		`AuthName "Members Only"`,
		"AuthUserFile " + passwdFile,
		"require valid-user",
		htaccessEnd,
	}, "\n")
	return patchHtaccess(filepath.Join(dir, ".htaccess"), block, chown)
}

// removeFolderAuth strips the ISPConfig block from a folder's .htaccess and
// deletes its .htpasswd. The .htaccess itself is kept when it still holds
// operator rules.
func removeFolderAuth(docroot, relPath string) error {
	dir := docroot
	if relPath != "" {
		dir = filepath.Join(docroot, filepath.Clean("/"+strings.Trim(relPath, "/")))
		if err := safeSitePath(dir, docroot); err != nil {
			return err
		}
	}
	if err := os.Remove(filepath.Join(dir, ".htpasswd")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return patchHtaccess(filepath.Join(dir, ".htaccess"), "", nil)
}

// patchHtaccess replaces the ISPConfig-delimited block of an .htaccess with
// block (an empty block removes it), preserving everything outside the
// markers. The file is deleted when nothing is left.
func patchHtaccess(path, block string, chown func(path string) error) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	rest := stripBlock(string(existing))
	var out string
	switch {
	case block == "" && strings.TrimSpace(rest) == "":
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	case block == "":
		out = rest
	case strings.TrimSpace(rest) == "":
		out = block + "\n"
	default:
		out = strings.TrimRight(rest, "\n") + "\n" + block + "\n"
	}
	if err := writeIfChanged(path, []byte(out), 0o644); err != nil {
		return err
	}
	if chown != nil {
		return chown(path)
	}
	return nil
}

// stripBlock removes the marker-delimited ISPConfig block from s. An
// unterminated opening marker truncates at the marker rather than silently
// keeping a half block that Apache would then reject.
func stripBlock(s string) string {
	start := strings.Index(s, htaccessBegin)
	if start < 0 {
		return s
	}
	end := strings.Index(s[start:], htaccessEnd)
	if end < 0 {
		return s[:start]
	}
	tail := s[start+end+len(htaccessEnd):]
	return s[:start] + strings.TrimPrefix(tail, "\n")
}

// htpasswdHash returns a value usable in a .htpasswd line. A stored value
// that already looks like a crypt hash is passed through; anything else is
// treated as a plaintext password and bcrypt-hashed.
func htpasswdHash(stored string) (string, error) {
	s := strings.TrimSpace(stored)
	if s == "" {
		return "", fmt.Errorf("apache2: empty .htpasswd password")
	}
	for _, prefix := range []string{"$2y$", "$2a$", "$2b$", "$apr1$", "$1$", "$5$", "$6$", "{SHA}"} {
		if strings.HasPrefix(s, prefix) {
			return s, nil
		}
	}
	h, err := bcrypt.GenerateFromPassword([]byte(s), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("apache2: hashing .htpasswd password: %w", err)
	}
	return string(h), nil
}

// writeIfChanged writes content only when it differs from what is on disk,
// so an unchanged site never touches mtimes or triggers a reload.
func writeIfChanged(path string, content []byte, mode os.FileMode) error {
	if old, err := os.ReadFile(path); err == nil && subtle.ConstantTimeCompare(old, content) == 1 {
		return nil
	}
	return os.WriteFile(path, content, mode)
}

// webFolder applies the auth files of one web_folder row.
func (p *Plugin) webFolder(_ context.Context, _ string, data engine.Data) error {
	return p.applyFolder(row(data.New))
}

// webFolderDelete drops the auth files of a removed web_folder row.
func (p *Plugin) webFolderDelete(_ context.Context, _ string, data engine.Data) error {
	old := row(data.Old)
	docroot, err := p.folderDocroot(old.num("parent_domain_id"))
	if err != nil || docroot == "" {
		return err
	}
	return removeFolderAuth(docroot, old.str("path"))
}

// webFolderUser re-applies the folder its user row belongs to; the .htpasswd
// is regenerated from every remaining user, so insert, update and delete all
// take the same path.
func (p *Plugin) webFolderUser(_ context.Context, _ string, data engine.Data) error {
	r := row(data.New)
	if len(r) == 0 {
		r = row(data.Old)
	}
	folder, err := p.loadFolder(r.num("web_folder_id"))
	if err != nil || folder == nil {
		return err
	}
	return p.applyFolder(folder)
}

// applyFolder writes (or, for an inactive folder, removes) the .htaccess and
// .htpasswd of one web_folder row.
func (p *Plugin) applyFolder(f row) error {
	if len(f) == 0 {
		return nil
	}
	docroot, err := p.folderDocroot(f.num("parent_domain_id"))
	if err != nil || docroot == "" {
		return err
	}
	if f.str("active") == "n" {
		return removeFolderAuth(docroot, f.str("path"))
	}
	users, err := p.loadFolderUsers(f.num("web_folder_id"))
	if err != nil {
		return err
	}
	if len(users) == 0 {
		// A protected folder with no users would lock everyone out with a
		// prompt nobody can answer; PHP removes the protection instead.
		return removeFolderAuth(docroot, f.str("path"))
	}
	return writeFolderAuth(docroot, folderAuth{path: f.str("path"), users: users}, nil)
}

// folderDocroot resolves the document root of the site owning a folder.
func (p *Plugin) folderDocroot(domainID int64) (string, error) {
	if p.db == nil || domainID == 0 {
		return "", nil
	}
	var d struct{ DocumentRoot string }
	err := p.db.Table("web_domain").Select("document_root").
		Where("domain_id = ?", domainID).Take(&d).Error
	if err != nil {
		return "", fmt.Errorf("apache2: loading document root of domain %d: %w", domainID, err)
	}
	return strings.TrimSuffix(d.DocumentRoot, "/"), nil
}

// loadFolder reads one web_folder row.
func (p *Plugin) loadFolder(folderID int64) (row, error) {
	if p.db == nil || folderID == 0 {
		return nil, nil
	}
	var raw map[string]any
	err := p.db.Table("web_folder").Where("web_folder_id = ?", folderID).Take(&raw).Error
	if err != nil {
		return nil, fmt.Errorf("apache2: loading web_folder %d: %w", folderID, err)
	}
	return row(raw), nil
}

// loadFolderUsers reads the active users of one protected folder.
func (p *Plugin) loadFolderUsers(folderID int64) ([]folderUser, error) {
	if p.db == nil || folderID == 0 {
		return nil, nil
	}
	var raw []map[string]any
	err := p.db.Table("web_folder_user").
		Where("web_folder_id = ? AND active = 'y'", folderID).Find(&raw).Error
	if err != nil {
		return nil, fmt.Errorf("apache2: loading users of web_folder %d: %w", folderID, err)
	}
	users := make([]folderUser, 0, len(raw))
	for _, r := range raw {
		users = append(users, folderUser{name: row(r).str("username"), hash: row(r).str("password")})
	}
	return users, nil
}

// randomSuffix returns a short URL-safe random string, used to name the
// temporary vhost backup of a config-test rollback.
func randomSuffix() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "tmp"
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
