package config

import "testing"

func TestLoadFromEnvUnifiedIOPSCanBeOverriddenPerDirection(t *testing.T) {
	t.Setenv("CONTAINER_IOPS_LIMIT", "900")
	t.Setenv("CONTAINER_READ_IOPS_LIMIT", "700")
	cfg := GetDefaultConfig()
	LoadFromEnv(cfg)

	if cfg.ContainerReadIOPSLimit != 700 {
		t.Fatalf("read IOPS = %d, want 700", cfg.ContainerReadIOPSLimit)
	}
	if cfg.ContainerWriteIOPSLimit != 900 {
		t.Fatalf("write IOPS = %d, want 900", cfg.ContainerWriteIOPSLimit)
	}
}

func TestLoadFromEnvReconcileInterval(t *testing.T) {
	t.Setenv("RECONCILE_INTERVAL", "120")
	cfg := GetDefaultConfig()
	LoadFromEnv(cfg)
	if cfg.ReconcileInterval != 120 {
		t.Fatalf("reconcile interval = %d, want 120", cfg.ReconcileInterval)
	}
}
