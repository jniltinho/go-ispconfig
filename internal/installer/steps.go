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
		bindBaseStep{},
		powerDNSStep{},
		ftpStep{},
		getmailStep{},
		acmeStep{},
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
		bindBaseStep{},
		systemdStep{},
	}
}
