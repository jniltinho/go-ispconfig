// Package installer implements the `go-ispconfig install` / `uninstall`
// orchestration: distro detection, answers resolution and the idempotent
// step pipeline that turns a clean Debian/Ubuntu host into a running panel
// (Go port of install/install.php + installer_base.lib.php, nginx+bind
// scope only).
package installer

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Profile is the static per-distro data set (Go port of the nginx/bind
// subset of install/dist/conf/*.conf.php). The five supported distros are
// identical except for the PHP-FPM version (design D2), so profiles are the
// base profile plus a PHP version override.
type Profile struct {
	// ID is the distro key: debian11|debian12|debian13|ubuntu22.04|ubuntu24.04.
	ID string
	// Name is the human-readable distro name for logs.
	Name string
	// PHPVersion is the distro's default PHP-FPM version ("8.3").
	PHPVersion string

	// Packages always installed (nginx, bind, mariadb, redis).
	Packages []string

	// Service unit names.
	NginxService  string
	BindService   string
	MySQLService  string
	PHPFPMService string
	RedisService  string

	// nginx paths (dist/conf $conf['nginx']).
	NginxConfigDir       string
	NginxVhostConfDir    string
	NginxVhostEnabledDir string

	// Apache paths and unit, used when the operator picks apache2 as the
	// web server (Answers.WebServer). Both web servers are described here
	// because the profile is built before the answers are known.
	ApacheService         string
	ApacheConfigDir       string
	ApacheVhostConfDir    string
	ApacheVhostEnabledDir string
	NginxUser             string
	NginxGroup            string

	// bind paths (dist/conf $conf['bind']).
	BindUser             string
	BindGroup            string
	BindZonefilesDir     string
	NamedConfPath        string
	NamedConfLocalPath   string
	NamedConfOptionsPath string

	// php-fpm paths (dist/conf $conf['nginx']['php_fpm_*']).
	PHPFPMIniPath   string
	PHPFPMPoolDir   string
	PHPFPMSocketDir string

	// WebsiteBasedir is $conf['web']['website_basedir'].
	WebsiteBasedir string
}

// PHPFPMPackage is the distro php-fpm apt package (php8.3-fpm).
func (p *Profile) PHPFPMPackage() string { return "php" + p.PHPVersion + "-fpm" }

// phpExtensions are the extensions any mainstream PHP application expects.
// php-fpm alone ships none of them, so a hosted WordPress/Nextcloud dies on
// "missing the MySQL extension" right after provisioning succeeds.
var phpExtensions = []string{"mysql", "curl", "gd", "mbstring", "xml", "zip", "intl"}

// PHPPackages is the php-fpm package plus the extensions hosted sites need.
func (p *Profile) PHPPackages() []string {
	pkgs := []string{p.PHPFPMPackage()}
	for _, ext := range phpExtensions {
		pkgs = append(pkgs, "php"+p.PHPVersion+"-"+ext)
	}
	return pkgs
}

// PowerDNS package/unit names, identical on all five supported distros.
const (
	// PowerDNSService is the unit shipped by pdns-server.
	PowerDNSService = "pdns"
	// BindPackage is dropped from the package set on the powerdns path:
	// both daemons would bind port 53.
	BindPackage = "bind9"
)

// powerDNSPackages is the gmysql-backed authoritative server package set
// (same names on Debian 11-13 and Ubuntu 22.04/24.04).
var powerDNSPackages = []string{"pdns-server", "pdns-backend-mysql"}

// supportedPHP maps distro id -> default PHP-FPM version, verified against
// install/dist/conf/{debian110,debian120,debian130,ubuntu2204,ubuntu2404}.conf.php.
var supportedPHP = map[string]string{
	"debian11":    "7.4",
	"debian12":    "8.2",
	"debian13":    "8.4",
	"ubuntu22.04": "8.1",
	"ubuntu24.04": "8.3",
}

// SupportedDistros returns the sorted list of supported distro ids.
func SupportedDistros() []string {
	ids := make([]string, 0, len(supportedPHP))
	for id := range supportedPHP {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// profileFor builds the full profile for a supported distro id.
func profileFor(id, name string) *Profile {
	php := supportedPHP[id]
	return &Profile{
		ID:         id,
		Name:       name,
		PHPVersion: php,
		// redis-server backs the asynq task queue (foundation D12); all
		// five distros ship it under the same package/unit name.
		Packages: []string{"nginx", "bind9", "mariadb-server", "redis-server"},

		NginxService: "nginx",
		// The bind9 package ships named.service with bind9.service as an
		// alias on every supported distro; `systemctl enable` refuses
		// aliases, so the real unit name is used.
		BindService:   "named",
		MySQLService:  "mariadb",
		PHPFPMService: "php" + php + "-fpm",
		RedisService:  "redis-server",

		NginxConfigDir:       "/etc/nginx",
		NginxVhostConfDir:    "/etc/nginx/sites-available",
		NginxVhostEnabledDir: "/etc/nginx/sites-enabled",

		ApacheService:         "apache2",
		ApacheConfigDir:       "/etc/apache2",
		ApacheVhostConfDir:    "/etc/apache2/sites-available",
		ApacheVhostEnabledDir: "/etc/apache2/sites-enabled",
		NginxUser:             "www-data",
		NginxGroup:            "www-data",

		BindUser:             "root",
		BindGroup:            "bind",
		BindZonefilesDir:     "/etc/bind",
		NamedConfPath:        "/etc/bind/named.conf",
		NamedConfLocalPath:   "/etc/bind/named.conf.local",
		NamedConfOptionsPath: "/etc/bind/named.conf.options",

		PHPFPMIniPath:   "/etc/php/" + php + "/fpm/php.ini",
		PHPFPMPoolDir:   "/etc/php/" + php + "/fpm/pool.d",
		PHPFPMSocketDir: "/var/lib/php" + php + "-fpm",

		WebsiteBasedir: "/var/www",
	}
}

// DetectDistro reads an os-release file (normally /etc/os-release) and maps
// ID + VERSION_ID to a supported profile. Unsupported distros return an
// error naming what was detected and the supported list.
func DetectDistro(osReleasePath string) (*Profile, error) {
	data, err := os.ReadFile(osReleasePath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", osReleasePath, err)
	}
	fields := parseOSRelease(string(data))
	distroID, version := fields["ID"], fields["VERSION_ID"]

	var id string
	switch distroID {
	case "debian":
		// Debian VERSION_ID is the major only ("12").
		id = "debian" + version
	case "ubuntu":
		id = "ubuntu" + version
	}
	if _, ok := supportedPHP[id]; !ok {
		return nil, fmt.Errorf("unsupported distribution %q version %q; supported: %s",
			distroID, version, strings.Join(SupportedDistros(), ", "))
	}
	name := fields["PRETTY_NAME"]
	if name == "" {
		name = id
	}
	return profileFor(id, name), nil
}

// parseOSRelease parses the KEY=value (optionally quoted) lines of an
// os-release file.
func parseOSRelease(content string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[k] = strings.Trim(v, `"'`)
	}
	return out
}
