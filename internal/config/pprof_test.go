package config

import "testing"

func TestIsPprofEnabled_PprofOnly(t *testing.T) {
	// pprof is self-contained and does not depend on the rest of the
	// observability stack (OTLP, tracing, metrics), so it must be enableable
	// without observability.enabled (which would otherwise force an OTLP
	// endpoint requirement).
	cfg := ObservabilityConfig{
		Enabled: false,
		Pprof:   PprofConfig{Enabled: true},
	}

	if !cfg.IsPprofEnabled() {
		t.Error("expected IsPprofEnabled()=true for pprof-only config, got false")
	}
}

func TestIsPprofEnabled_Disabled(t *testing.T) {
	cfg := ObservabilityConfig{
		Enabled: true,
		Pprof:   PprofConfig{Enabled: false},
	}

	if cfg.IsPprofEnabled() {
		t.Error("expected IsPprofEnabled()=false when pprof disabled, got true")
	}
}
