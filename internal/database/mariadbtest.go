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

	for i := 0; i < 60; i++ {
		if exec.Command("docker", "exec", name, "mariadb", "-uroot", "-proot", "-e", "SELECT 1").Run() == nil {
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
// stock mariadb client.
func MariaDBExec(t *testing.T, container, sql string) {
	t.Helper()
	out, err := exec.Command("docker", "exec", container, "mariadb", "-uroot", "-proot", "-e", sql).CombinedOutput()
	if err != nil {
		t.Fatalf("mariadb -e %q: %v: %s", sql, err, out)
	}
}
