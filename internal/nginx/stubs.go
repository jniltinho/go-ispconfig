package nginx

// Temporary stubs for handlers implemented by later tasks of
// add-web-nginx-module (3.2–3.6). Each disappears when its task lands.

import (
	"context"

	"go-ispconfig/internal/engine"
)

// applyVhost renders, merges and activates the vhost (tasks 3.2–3.4).
func (p *Plugin) applyVhost(context.Context, site) error { return nil }

// webDomainDelete removes a site (task 3.5).
func (p *Plugin) webDomainDelete(context.Context, string, engine.Data) error { return nil }

// clientDelete tears down all sites of a deleted client (task 3.5).
func (p *Plugin) clientDelete(context.Context, string, engine.Data) error { return nil }

// webFolderUpdate maintains folder protection (task 3.6).
func (p *Plugin) webFolderUpdate(context.Context, string, engine.Data) error { return nil }

// webFolderDelete removes folder protection (task 3.6).
func (p *Plugin) webFolderDelete(context.Context, string, engine.Data) error { return nil }

// webFolderUser maintains .htpasswd users (task 3.6).
func (p *Plugin) webFolderUser(context.Context, string, engine.Data) error { return nil }
