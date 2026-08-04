package installer

// InstallSteps is the full ordered pipeline of a fresh/converging install
// (design D1).
func InstallSteps() []Step {
	return []Step{
		preflightStep{},
		packagesStep{},
		mariadbStep{},
		serverIPStep{},
		userStep{},
		configTomlStep{},
		tlsCertStep{},
		nginxBaseStep{},
		apache2Step{},
		bindBaseStep{},
		powerDNSStep{},
		ftpStep{},
		fail2banStep{},
		vmailStep{},
		postfixStep{},
		dovecotStep{},
		rspamdStep{},
		getmailStep{},
		systemdStep{},
		summaryStep{},
	}
}

// UpdateSteps is the `install --update` subset (design D9): re-render base
// configs and units, restart. It deliberately contains no database,
// config.toml, certificate or admin-seed step — those are never touched by
// an update.
func UpdateSteps() []Step {
	return []Step{
		preflightStep{},
		nginxBaseStep{},
		apache2Step{},
		bindBaseStep{},
		systemdStep{},
	}
}
