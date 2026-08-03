package monitor

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeProber struct {
	tcp   map[int]bool
	udp   map[int]bool
	ftp   map[int]bool
	mysql bool
}

func (f fakeProber) CheckTCP(_ string, port int) bool {
	if f.tcp == nil {
		return false
	}
	return f.tcp[port]
}
func (f fakeProber) CheckUDP(_ string, port int) bool {
	if f.udp == nil {
		return false
	}
	return f.udp[port]
}
func (f fakeProber) CheckFTP(_ string, port int) bool {
	if f.ftp == nil {
		return f.CheckTCP("", port)
	}
	return f.ftp[port]
}
func (f fakeProber) CheckMySQL(string) bool { return f.mysql }

func TestCollectServices_allUnused(t *testing.T) {
	data, state := CollectServices(ServiceFlags{}, fakeProber{}, "")
	assert.Equal(t, "ok", state)
	for _, k := range []string{"webserver", "ftpserver", "smtpserver", "pop3server", "imapserver", "bindserver", "mysqlserver"} {
		assert.Equal(t, -1, data[k], k)
	}
}

func TestCollectServices_webDownIsError(t *testing.T) {
	data, state := CollectServices(ServiceFlags{WebServer: 1}, fakeProber{tcp: map[int]bool{80: false}}, "")
	assert.Equal(t, "error", state)
	assert.Equal(t, 0, data["webserver"])
}

func TestCollectServices_webUp(t *testing.T) {
	data, state := CollectServices(ServiceFlags{WebServer: 1}, fakeProber{tcp: map[int]bool{80: true}}, "")
	assert.Equal(t, "ok", state)
	assert.Equal(t, 1, data["webserver"])
}

func TestCollectServices_dnsUnusedDoesNotError(t *testing.T) {
	data, state := CollectServices(
		ServiceFlags{WebServer: 1, DNSServer: 0},
		fakeProber{tcp: map[int]bool{80: true}, udp: map[int]bool{53: false}},
		"",
	)
	assert.Equal(t, "ok", state)
	assert.Equal(t, -1, data["bindserver"])
	assert.Equal(t, 1, data["webserver"])
}

func TestCollectServices_mailPorts(t *testing.T) {
	p := fakeProber{tcp: map[int]bool{25: true, 110: false, 143: true}}
	data, state := CollectServices(ServiceFlags{MailServer: 1}, p, "")
	assert.Equal(t, "error", state)
	assert.Equal(t, 1, data["smtpserver"])
	assert.Equal(t, 0, data["pop3server"])
	assert.Equal(t, 1, data["imapserver"])
}

func TestCollectServices_mysql(t *testing.T) {
	data, state := CollectServices(ServiceFlags{DBServer: 1}, fakeProber{mysql: true}, "x")
	assert.Equal(t, "ok", state)
	assert.Equal(t, 1, data["mysqlserver"])
}

func TestNetProber_fakeListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close() //nolint:errcheck // test listener
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	p := NetProber{}
	assert.True(t, p.CheckTCP("127.0.0.1", port))
	assert.False(t, p.CheckTCP("127.0.0.1", 1)) // privileged/closed
}
