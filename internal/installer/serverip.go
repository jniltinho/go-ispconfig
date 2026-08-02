package installer

import (
	"context"
	"fmt"
	"net"

	"go-ispconfig/internal/model"
)

// serverIPStep detects the host's global unicast addresses and inserts the
// missing server_ip rows for the local server (port of detect_ips /
// get_host_ips). The server row itself is created by the foundation Seed in
// the mariadb step (web_server=1, dns_server=1, default config INI).
type serverIPStep struct{}

// Name identifies the step in the pipeline log.
func (serverIPStep) Name() string { return "server-ips" }

// Run inserts one server_ip row per detected address not yet recorded.
func (serverIPStep) Run(_ context.Context, st *State) error {
	var server model.Server
	if err := st.DB.Where("active = 1").First(&server).Error; err != nil {
		return fmt.Errorf("loading server row: %w", err)
	}

	ips := st.HostIPs()
	if len(ips) == 0 {
		return Skip("no global IP addresses detected")
	}

	added := 0
	for _, ip := range ips {
		ipType := "IPv4"
		if ip.To4() == nil {
			ipType = "IPv6"
		}
		var n int64
		err := st.DB.Model(&model.ServerIP{}).
			Where("server_id = ? AND ip_address = ?", server.ServerID, ip.String()).
			Count(&n).Error
		if err != nil {
			return fmt.Errorf("checking server_ip %s: %w", ip, err)
		}
		if n > 0 {
			continue
		}
		row := model.ServerIP{
			SysUserID: 1, SysGroupID: 1,
			SysPermUser: "riud", SysPermGroup: "riud", SysPermOther: "r",
			ServerID:        server.ServerID,
			IPType:          ipType,
			IPAddress:       ip.String(),
			Virtualhost:     "y",
			VirtualhostPort: "80,443",
		}
		if err := st.DB.Create(&row).Error; err != nil {
			return fmt.Errorf("inserting server_ip %s: %w", ip, err)
		}
		added++
	}
	if added == 0 {
		return Skip("all detected IPs already recorded")
	}
	st.logf("  recorded %d IP address(es)", added)
	return nil
}

// detectHostIPs enumerates global unicast IPv4/IPv6 addresses of all
// interfaces (loopback and link-local excluded).
func detectHostIPs() []net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var ips []net.IP
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		// IsGlobalUnicast excludes loopback, link-local and multicast but
		// keeps RFC1918/ULA addresses — a LAN server records its LAN IP.
		if !ok || !ipNet.IP.IsGlobalUnicast() {
			continue
		}
		ips = append(ips, ipNet.IP)
	}
	return ips
}
