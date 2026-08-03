package monitor

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"time"

	// MySQL driver for the MariaDB service probe.
	_ "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// ServiceFlags mirrors the server role columns used by monitorServices.
type ServiceFlags struct {
	WebServer  int8
	FileServer int8
	MailServer int8
	DNSServer  int8
	DBServer   int8
}

// Prober performs TCP/UDP/FTP/MySQL liveness checks (injectable for tests).
type Prober interface {
	CheckTCP(host string, port int) bool
	CheckUDP(host string, port int) bool
	CheckFTP(host string, port int) bool
	CheckMySQL(dsn string) bool
}

// NetProber is the production prober using the network stack.
type NetProber struct {
	// DialTimeout for TCP/UDP attempts.
	DialTimeout time.Duration
	// MySQLDSN used when probing MariaDB (empty = skip with false).
	MySQLDSN string
}

func (p NetProber) timeout() time.Duration {
	if p.DialTimeout > 0 {
		return p.DialTimeout
	}
	return 2 * time.Second
}

// CheckTCP opens a TCP connection; for port 80 also reads a short HTTP response.
func (p NetProber) CheckTCP(host string, port int) bool {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, p.timeout())
	if err != nil {
		return false
	}
	defer conn.Close() //nolint:errcheck // probe connection
	if port == 80 {
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		_, _ = conn.Write([]byte(
			"GET / HTTP/1.1\r\nHost: localhost\r\nUser-Agent: Mozilla/5.0 (ISPConfig monitor)\r\n" +
				"Accept: application/xml,application/xhtml+xml,text/html\r\nConnection: Close\r\n\r\n",
		))
		buf := make([]byte, 10)
		if _, err := conn.Read(buf); err != nil {
			return false
		}
	}
	return true
}

// CheckUDP dials UDP (best-effort like PHP fsockopen udp).
func (p NetProber) CheckUDP(host string, port int) bool {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("udp", addr, p.timeout())
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// CheckFTP dials TCP 21 (PHP ftp_connect parity without auth).
func (p NetProber) CheckFTP(host string, port int) bool {
	return p.CheckTCP(host, port)
}

// CheckMySQL pings the database when MySQLDSN is set.
func (p NetProber) CheckMySQL(dsn string) bool {
	if dsn == "" {
		dsn = p.MySQLDSN
	}
	if dsn == "" {
		return false
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return false
	}
	defer db.Close() //nolint:errcheck // probe connection
	db.SetConnMaxLifetime(p.timeout())
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout())
	defer cancel()
	return db.PingContext(ctx) == nil
}

// CollectServices ports monitor_tools::monitorServices. Values are 1/0/-1
// (up/down/unused). State is ok or error only.
func CollectServices(flags ServiceFlags, prober Prober, mysqlDSN string) (map[string]any, string) {
	if prober == nil {
		prober = NetProber{}
	}
	state := "ok"
	data := map[string]any{
		"webserver":   -1,
		"ftpserver":   -1,
		"smtpserver":  -1,
		"pop3server":  -1,
		"imapserver":  -1,
		"bindserver":  -1,
		"mysqlserver": -1,
	}

	set := func(key string, ok bool) {
		if ok {
			data[key] = 1
		} else {
			data[key] = 0
			state = "error"
		}
	}

	if flags.WebServer == 1 {
		set("webserver", prober.CheckTCP("localhost", 80))
	}
	if flags.FileServer == 1 {
		set("ftpserver", prober.CheckFTP("localhost", 21))
	}
	if flags.MailServer == 1 {
		set("smtpserver", prober.CheckTCP("localhost", 25))
		set("pop3server", prober.CheckTCP("localhost", 110))
		set("imapserver", prober.CheckTCP("localhost", 143))
	}
	if flags.DNSServer == 1 {
		set("bindserver", prober.CheckUDP("localhost", 53))
	}
	if flags.DBServer == 1 {
		set("mysqlserver", prober.CheckMySQL(mysqlDSN))
	}
	return data, state
}

// FlagsFromServer maps a server row to ServiceFlags.
func FlagsFromServer(s model.Server) ServiceFlags {
	return ServiceFlags{
		WebServer:  s.WebServer,
		FileServer: s.FileServer,
		MailServer: s.MailServer,
		DNSServer:  s.DNSServer,
		DBServer:   s.DBServer,
	}
}

// RunServicesCollector loads the server row and stores a services sample.
func RunServicesCollector(ctx context.Context, db *gorm.DB, serverID uint32, prober Prober, mysqlDSN string) error {
	var srv model.Server
	if err := db.WithContext(ctx).Where("server_id = ?", serverID).First(&srv).Error; err != nil {
		// PHP falls back to db_server=1 when the server row is missing.
		if err == gorm.ErrRecordNotFound {
			data, state := CollectServices(ServiceFlags{DBServer: 1}, prober, mysqlDSN)
			return Store(ctx, db, serverID, "services", data, state, 0)
		}
		return err
	}
	data, state := CollectServices(FlagsFromServer(srv), prober, mysqlDSN)
	return Store(ctx, db, serverID, "services", data, state, 0)
}
