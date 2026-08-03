//go:build integration

package database

import (
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// StartMariaDB runs a throwaway mariadb:11 docker container on an ephemeral
// local port for integration tests and returns the root DSN prefix
// ("root:root@tcp(127.0.0.1:PORT)") plus the container name for
// MariaDBExec. suffix keeps container names unique across test packages and
// the random part across runs (PIDs recycle; a crashed run may leave a
// container behind); the container is removed via t.Cleanup.
func StartMariaDB(t *testing.T, suffix string) (dsnPrefix, container string) {
	t.Helper()
	name := fmt.Sprintf("goisp-%s-test-%d-%08x", suffix, os.Getpid(), rand.Uint32())

	out, err := exec.Command("docker", "run", "--rm", "-d", "--name", name,
		"-e", "MARIADB_ROOT_PASSWORD=root", "-p", "127.0.0.1::3306", "mariadb:11").CombinedOutput()
	if err != nil {
		t.Fatalf("docker run: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })

	// Probe over TCP (-h 127.0.0.1), never the unix socket: the entrypoint's
	// temporary init server runs --skip-networking and answers on the socket,
	// so a socket probe returns a DSN for a server that is about to be shut
	// down — the caller's first query then dies with "driver: bad connection".
	for i := 0; i < 60; i++ {
		if exec.Command("docker", "exec", name, "mariadb", "-h", "127.0.0.1", "-uroot", "-proot", "-e", "SELECT 1").Run() == nil {
			portOut, err := exec.Command("docker", "port", name, "3306/tcp").Output()
			if err != nil {
				t.Fatalf("docker port: %v", err)
			}
			addr := strings.TrimSpace(strings.Split(string(portOut), "\n")[0])
			return "root:root@tcp(" + addr + ")", name
		}
		time.Sleep(time.Second)
	}
	t.Fatal("mariadb container did not become ready in 60s")
	return "", ""
}

// MariaDBExec runs a SQL statement inside the test container with the
// stock mariadb client. Retries briefly: right after StartMariaDB the
// server may still bounce the next connection (CI flake with socket
// "Can't connect ... mysqld.sock").
func MariaDBExec(t *testing.T, container, sql string) {
	t.Helper()
	var out []byte
	var err error
	for attempt := 0; attempt < 30; attempt++ {
		out, err = exec.Command("docker", "exec", container, "mariadb", "-uroot", "-proot", "-e", sql).CombinedOutput()
		if err == nil {
			return
		}
		msg := string(out) + err.Error()
		if !strings.Contains(msg, "Can't connect") && !strings.Contains(msg, "Connection refused") &&
			!strings.Contains(msg, "is not allowed to connect") && !strings.Contains(msg, "server has gone away") {
			break
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("mariadb -e %q: %v: %s", sql, err, out)
}
