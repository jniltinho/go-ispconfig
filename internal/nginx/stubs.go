package nginx

// Temporary stubs for handlers implemented by later tasks of
// add-web-nginx-module (3.2–3.6). Each disappears when its task lands.

import (
	"context"

	"go-ispconfig/internal/engine"
)

// webFolderUpdate maintains folder protection (task 3.6).
func (p *Plugin) webFolderUpdate(context.Context, string, engine.Data) error { return nil }

// webFolderDelete removes folder protection (task 3.6).
func (p *Plugin) webFolderDelete(context.Context, string, engine.Data) error { return nil }

// webFolderUser maintains .htpasswd users (task 3.6).
func (p *Plugin) webFolderUser(context.Context, string, engine.Data) error { return nil }
