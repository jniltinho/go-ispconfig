package installer

// InstallSteps is the full ordered pipeline of a fresh/converging install
// (design D1). Steps are appended here as they are implemented.
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
		summaryStep{},
	}
}
