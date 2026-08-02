package nginx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"gorm.io/gorm"

	"go-ispconfig/internal/engine"
)

// folderAuthPath computes the on-disk folder path of a protected folder and
// applies the PHP safety checks (no .., ./ or backslashes; must stay inside
// the docroot).
func folderAuthPath(website row, folderPath string) (string, error) {
	webFolder := "web"
	if website.str("type") == "vhostsubdomain" || website.str("type") == "vhostalias" {
		webFolder = website.str("web_folder")
	}
	p := trimSlashes(folderPath)
	full := website.str("document_root") + "/" + webFolder + "/" + p
	if !strings.HasSuffix(full, "/") {
		full += "/"
	}
	if strings.Contains(full, "..") || strings.Contains(full, "./") || strings.Contains(full, `\`) {
		return "", fmt.Errorf("nginx: folder path %q contains traversal characters", full)
	}
	docroot := website.str("document_root")
	if docroot == "" || !strings.HasPrefix(full, docroot+"/") {
		return "", fmt.Errorf("nginx: folder path %q is outside the docroot", full)
	}
	return full, nil
}

// validHtpasswdField rejects newlines/carriage returns (which would inject
// extra .htpasswd lines) and, for the username, a colon (the field
// separator). username and password come from the panel, so this is a trust
// boundary the daemon enforces even though the API also validates.
func validHtpasswdField(user, password string) error {
	if strings.ContainsAny(user, ":\r\n") {
		return fmt.Errorf("nginx: invalid auth username %q", user)
	}
	if strings.ContainsAny(password, "\r\n") {
		return fmt.Errorf("nginx: invalid auth password for user %q", user)
	}
	return nil
}

// upsertHtpasswdLine adds or replaces the user's line in an .htpasswd file.
func upsertHtpasswdLine(file, user, password string) error {
	if err := validHtpasswdField(user, password); err != nil {
		return err
	}
	lines, err := readLines(file)
	if err != nil {
		return err
	}
	replaced := false
	for i, l := range lines {
		if strings.HasPrefix(l, user+":") {
			lines[i] = user + ":" + password
			replaced = true
		}
	}
	if !replaced {
		lines = append(lines, user+":"+password)
	}
	return writeLines(file, lines)
}

// removeHtpasswdLine drops the user's line from an .htpasswd file.
func removeHtpasswdLine(file, user string) error {
	lines, err := readLines(file)
	if err != nil {
		return err
	}
	kept := lines[:0]
	for _, l := range lines {
		if !strings.HasPrefix(l, user+":") {
			kept = append(kept, l)
		}
	}
	return writeLines(file, kept)
}

func readLines(file string) ([]string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

func writeLines(file string, lines []string) error {
	content := ""
	if len(lines) > 0 {
		content = strings.Join(lines, "\n") + "\n"
	}
	return os.WriteFile(file, []byte(content), 0o640)
}

// maintainFolderAuth is the testable core of the web_folder_user handler:
// it creates the folder and its .htpasswd file and applies one user change
// (port of web_folder_user()).
func (p *Plugin) maintainFolderAuth(ctx context.Context, event string, data engine.Data, folder, website row) error {
	folderPath, err := folderAuthPath(website, folder.str("path"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(folderPath, 0o755); err != nil {
		return fmt.Errorf("nginx: creating %s: %w", folderPath, err)
	}
	if err := p.chown(ctx, strings.TrimSuffix(folderPath, "/"),
		website.str("system_user"), website.str("system_group"), false); err != nil {
		return err
	}
	htpasswd := folderPath + ".htpasswd"
	if _, err := os.Stat(htpasswd); os.IsNotExist(err) {
		if err := writeLines(htpasswd, nil); err != nil {
			return fmt.Errorf("nginx: creating %s: %w", htpasswd, err)
		}
		if err := p.chown(ctx, htpasswd, website.str("system_user"), website.str("system_group"), false); err != nil {
			return err
		}
	}

	old, new := row(data.Old), row(data.New)
	// A renamed or deactivated user loses its old line first.
	if old.str("username") != "" &&
		(new.str("username") != old.str("username") || new.str("active") == "n") {
		if err := removeHtpasswdLine(htpasswd, old.str("username")); err != nil {
			return err
		}
	}
	if event == "web_folder_user_delete" {
		if old.str("username") != "" {
			if err := removeHtpasswdLine(htpasswd, old.str("username")); err != nil {
				return err
			}
		}
	} else if new.str("active") == "y" {
		if err := upsertHtpasswdLine(htpasswd, new.str("username"), new.str("password")); err != nil {
			return err
		}
	}
	return nil
}

// webFolderUser handles web_folder_user_{insert,update,delete}: auth file
// maintenance plus a re-render of the parent vhost so the auth_basic
// location appears/disappears.
func (p *Plugin) webFolderUser(ctx context.Context, event string, data engine.Data) error {
	ref := row(data.New)
	if event == "web_folder_user_delete" {
		ref = row(data.Old)
	}
	folder, err := p.loadTableRow("web_folder", "web_folder_id", ref.num("web_folder_id"))
	if err != nil || folder == nil {
		return err
	}
	website, err := p.loadTableRow("web_domain", "domain_id", folder.num("parent_domain_id"))
	if err != nil || website == nil {
		return err
	}
	if err := p.maintainFolderAuth(ctx, event, data, folder, website); err != nil {
		return err
	}
	return p.applyWebDomain(ctx, "update", website, website)
}

// webFolderUpdate handles web_folder_update: folder path moves take the
// .htpasswd file along, then the vhost is re-rendered.
func (p *Plugin) webFolderUpdate(ctx context.Context, _ string, data engine.Data) error {
	old, new := row(data.Old), row(data.New)
	website, err := p.loadTableRow("web_domain", "domain_id", new.num("parent_domain_id"))
	if err != nil || website == nil {
		return err
	}
	newPath, err := folderAuthPath(website, new.str("path"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(newPath, 0o755); err != nil {
		return fmt.Errorf("nginx: creating %s: %w", newPath, err)
	}
	if err := p.chown(ctx, strings.TrimSuffix(newPath, "/"),
		website.str("system_user"), website.str("system_group"), false); err != nil {
		return err
	}
	if old.str("path") != new.str("path") && old.str("path") != "" {
		oldPath, err := folderAuthPath(website, old.str("path"))
		if err == nil {
			if _, statErr := os.Stat(oldPath + ".htpasswd"); statErr == nil {
				if err := os.Rename(oldPath+".htpasswd", newPath+".htpasswd"); err != nil {
					return fmt.Errorf("nginx: moving .htpasswd: %w", err)
				}
			}
		}
	}
	return p.applyWebDomain(ctx, "update", website, website)
}

// webFolderDelete handles web_folder_delete: the .htpasswd file is removed
// and the vhost re-rendered without the protected location.
func (p *Plugin) webFolderDelete(ctx context.Context, _ string, data engine.Data) error {
	old := row(data.Old)
	website, err := p.loadTableRow("web_domain", "domain_id", old.num("parent_domain_id"))
	if err != nil || website == nil {
		return err
	}
	folderPath, err := folderAuthPath(website, old.str("path"))
	if err != nil {
		return err
	}
	if err := os.Remove(folderPath + ".htpasswd"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("nginx: removing %s.htpasswd: %w", folderPath, err)
	}
	return p.applyWebDomain(ctx, "update", website, website)
}

// loadTableRow fetches one row by primary key as a map (nil when missing).
func (p *Plugin) loadTableRow(table, pk string, id int64) (row, error) {
	if id == 0 {
		return nil, nil
	}
	var rec map[string]any
	err := p.db.Table(table).Where(pk+" = ?", id).Take(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("nginx: loading %s %d: %w", table, id, err)
	}
	return rec, nil
}
