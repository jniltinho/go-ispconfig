package client

import (
	"context"
	"strings"
)

// RequiredFunctions lists every legacy remote-API function the import
// engine calls. Preflight verifies all of them before any data is
// fetched.
var RequiredFunctions = []string{
	"client_get",
	"client_get_all",
	"sites_web_domain_get",
	"sites_web_folder_get",
	"sites_web_folder_user_get",
	"dns_zone_get",
	"dns_rr_get_all_by_zone",
	"dns_slave_get",
	"dns_templatezone_get_all",
	"server_get",
	"server_get_all",
}

// MissingGrantsError reports the required remote-API functions the
// remote_user does not have, in RequiredFunctions order.
type MissingGrantsError struct {
	// Missing lists the exact missing function names.
	Missing []string
}

// Error implements the error interface.
func (e *MissingGrantsError) Error() string {
	return "legacy: remote_user is missing required functions: " + strings.Join(e.Missing, ", ")
}

// Preflight calls get_function_list and verifies that every function in
// RequiredFunctions is available to the remote user, returning a
// *MissingGrantsError naming the exact missing functions. It must be run
// after Login and before any data fetch.
func (c *Client) Preflight(ctx context.Context) error {
	granted, err := c.GetFunctionList(ctx)
	if err != nil {
		return err
	}
	have := make(map[string]bool, len(granted))
	for _, fn := range granted {
		have[fn] = true
	}
	var missing []string
	for _, fn := range RequiredFunctions {
		if !have[fn] {
			missing = append(missing, fn)
		}
	}
	if len(missing) > 0 {
		return &MissingGrantsError{Missing: missing}
	}
	return nil
}
