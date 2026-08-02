package clientdb

import (
	"context"
	"net"
	"slices"
	"strings"
)

// hostList returns the MySQL account hosts a database is accessible from
// (port of getHostList, design D4): always localhost; with
// remote_access = 'y' the valid IPs of remote_ips, or '%' when none
// survive filtering. Unique and sorted.
func hostList(r row) []string {
	var hosts []string
	if r.str("remote_access") == "y" {
		for ip := range strings.SplitSeq(r.str("remote_ips"), ",") {
			ip = strings.TrimSpace(ip)
			if net.ParseIP(ip) != nil {
				hosts = append(hosts, ip)
			}
		}
		if len(hosts) == 0 {
			hosts = append(hosts, "%")
		}
	}
	hosts = append(hosts, "localhost")
	slices.Sort(hosts)
	return slices.Compact(hosts)
}

// unionHostLists unions the host lists of the given database records,
// unique with first-seen order preserved (PHP array_unique parity).
func unionHostLists(records []row) []string {
	var union []string
	seen := map[string]struct{}{}
	for _, r := range records {
		for _, h := range hostList(r) {
			if _, ok := seen[h]; !ok {
				seen[h] = struct{}{}
				union = append(union, h)
			}
		}
	}
	return union
}

// otherHostList returns the union of host lists of every other active
// database that still references userID as rw or ro user (port of
// getOtherHostList): DROP USER only happens for hosts absent here.
//
//nolint:unused // wired into dbUpdate/dbDelete by task 3.6
func (p *Plugin) otherHostList(ctx context.Context, databaseID, userID int64) ([]string, error) {
	var records []struct {
		RemoteAccess string
		RemoteIps    string
	}
	err := p.db.WithContext(ctx).Table("web_database").
		Select("remote_access, remote_ips").
		Where("(database_user_id = ? OR database_ro_user_id = ?) AND active = 'y' AND database_id != ?",
			userID, userID, databaseID).
		Find(&records).Error
	if err != nil {
		return nil, err
	}
	rows := make([]row, 0, len(records))
	for _, r := range records {
		rows = append(rows, row{"remote_access": r.RemoteAccess, "remote_ips": r.RemoteIps})
	}
	return unionHostLists(rows), nil
}
