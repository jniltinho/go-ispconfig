package cron

// Enabled reports whether the cron module and client-job runner should load
// for this server: web_server must be 1 and the module must not be disabled
// in config.toml (cron-module-events / design D1; port of PHP
// cron_plugin::onInstall requiring the web service).
func Enabled(webServer int8, disabled bool) bool {
	return webServer == 1 && !disabled
}
