//go:build integration

package queue

import (
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// StartRedis runs a throwaway redis:7-alpine docker container on an
// ephemeral local port for integration tests and returns its host:port
// address. suffix keeps container names unique across test packages and the
// random part across runs; the container is removed via t.Cleanup.
func StartRedis(t *testing.T, suffix string) string {
	t.Helper()
	name := fmt.Sprintf("goisp-%s-redis-%d-%08x", suffix, os.Getpid(), rand.Uint32())

	out, err := exec.Command("docker", "run", "--rm", "-d", "--name", name,
		"-p", "127.0.0.1::6379", "redis:7-alpine").CombinedOutput()
	if err != nil {
		t.Fatalf("docker run: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })

	for range 30 {
		if out, err := exec.Command("docker", "exec", name, "redis-cli", "ping").Output(); err == nil &&
			strings.HasPrefix(string(out), "PONG") {
			portOut, err := exec.Command("docker", "port", name, "6379/tcp").Output()
			if err != nil {
				t.Fatalf("docker port: %v", err)
			}
			return strings.TrimSpace(strings.Split(string(portOut), "\n")[0])
		}
		time.Sleep(time.Second)
	}
	t.Fatal("redis container did not become ready in 30s")
	return ""
}
