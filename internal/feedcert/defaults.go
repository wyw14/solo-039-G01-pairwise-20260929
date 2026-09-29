package feedcert

// DefaultSpecs returns the seeded acceptance windows for the grades the plant
// ships with. Additional grades can be registered at runtime.
func DefaultSpecs() []GradeSpec {
	return []GradeSpec{
		{
			Grade:        "baseline",
			Components:   map[string]Range{"ethanol": {Min: 0.88, Max: 0.95}},
			MaxWater:     0.08,
			MaxImpurity:  0.04,
			CertTTLHours: 24,
		},
		{
			Grade:        "premium",
			Components:   map[string]Range{"ethanol": {Min: 0.92, Max: 0.97}},
			MaxWater:     0.04,
			MaxImpurity:  0.015,
			CertTTLHours: 12,
		},
	}
}

// RegisterDefaults seeds the registry with the shipped grade windows.
func RegisterDefaults(registry *Registry) {
	for _, spec := range DefaultSpecs() {
		_ = registry.RegisterSpec(spec)
	}
}
