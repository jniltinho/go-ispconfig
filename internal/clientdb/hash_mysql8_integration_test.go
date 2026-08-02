//go:build integration

package clientdb

import (
	"database/sql"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestHashesAcceptedByMySQL8 proves the Sha2PasswordHash authentication
// string against a real MySQL 8: a user created with it must
// authenticate with the plaintext. (The native hash is validated against
// MariaDB elsewhere; MySQL 8.4 no longer loads mysql_native_password.)
func TestHashesAcceptedByMySQL8(t *testing.T) {
	name := fmt.Sprintf("goisp-my8-test-%d-%08x", os.Getpid(), rand.Uint32())
	out, err := exec.Command("docker", "run", "--rm", "-d", "--name", name,
		"-e", "MYSQL_ROOT_PASSWORD=root", "-p", "127.0.0.1::3306", "mysql:8").CombinedOutput()
	require.NoError(t, err, string(out))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })

	ready := false
	for range 90 {
		if exec.Command("docker", "exec", name, "mysql", "-uroot", "-proot", "-e", "SELECT 1").Run() == nil {
			ready = true
			break
		}
		time.Sleep(time.Second)
	}
	require.True(t, ready, "mysql:8 container did not become ready")
	portOut, err := exec.Command("docker", "port", name, "3306/tcp").Output()
	require.NoError(t, err)
	addr := strings.TrimSpace(strings.Split(string(portOut), "\n")[0])

	sha2, err := Sha2PasswordHash("s3cret-pw")
	require.NoError(t, err)
	mysqlExec := func(stmt string) {
		out, err := exec.Command("docker", "exec", name, "mysql", "-uroot", "-proot", "-e", stmt).CombinedOutput()
		require.NoError(t, err, "%s: %s", stmt, out)
	}
	mysqlExec("CREATE USER 'sha2u'@'%' IDENTIFIED WITH caching_sha2_password AS '" + sha2 + "'")

	db, err := sql.Open("mysql", "sha2u:s3cret-pw@tcp("+addr+")/")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.Ping(), "sha2 user must authenticate with its plaintext")
}
