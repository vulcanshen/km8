package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupIsolatedConfigDirs points ConfigDir + legacyConfigDir at
// TempDir-backed paths so migration tests don't touch the real
// $HOME/.config/{kbu,kbu}. Works by setting $XDG_CONFIG_HOME —
// both ConfigDir and legacyConfigDir honor it.
func setupIsolatedConfigDirs(t *testing.T) (newDir, oldDir string) {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	return filepath.Join(xdg, appName), filepath.Join(xdg, legacyAppName)
}

func TestMigrateLegacyConfigDir_NoLegacyDir(t *testing.T) {
	setupIsolatedConfigDirs(t)

	warn, err := MigrateLegacyConfigDir()
	if err != nil {
		t.Fatalf("MigrateLegacyConfigDir: %v", err)
	}
	if warn != "" {
		t.Errorf("expected empty warning for fresh install, got %q", warn)
	}
}

func TestMigrateLegacyConfigDir_NewDirAlreadyExists(t *testing.T) {
	newDir, oldDir := setupIsolatedConfigDirs(t)

	// Old exists with a marker file, new exists (already migrated
	// or user pre-created it) — migration must NOT overwrite.
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "config.yaml"), []byte("legacy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "config.yaml"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	warn, err := MigrateLegacyConfigDir()
	if err != nil {
		t.Fatalf("MigrateLegacyConfigDir: %v", err)
	}
	if warn != "" {
		t.Errorf("expected empty warning when new dir already exists, got %q", warn)
	}

	// Verify new dir was not clobbered
	data, err := os.ReadFile(filepath.Join(newDir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Errorf("new dir clobbered by migration: got %q, want %q", string(data), "new")
	}
}

func TestMigrateLegacyConfigDir_CopiesFilesAndSubdirs(t *testing.T) {
	newDir, oldDir := setupIsolatedConfigDirs(t)

	// Populate legacy dir with a mix of top-level files and a
	// subdirectory (mirrors the real config dir layout: config.yaml
	// + state.yaml + theme.yaml + optional log subdir).
	if err := os.MkdirAll(filepath.Join(oldDir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"config.yaml":      "default_context: orbstack\n",
		"state.yaml":       "kind: pods\n",
		"theme.yaml":       "background: '#000'\n",
		"logs/crash-1.log": "panic: nope\n",
	}
	for rel, content := range files {
		p := filepath.Join(oldDir, rel)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("seeding %s: %v", rel, err)
		}
	}

	warn, err := MigrateLegacyConfigDir()
	if err != nil {
		t.Fatalf("MigrateLegacyConfigDir: %v", err)
	}
	if warn == "" {
		t.Fatal("expected non-empty warning after successful migration")
	}
	if !strings.Contains(warn, oldDir) || !strings.Contains(warn, newDir) {
		t.Errorf("warning should mention both dirs: got %q", warn)
	}

	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(newDir, rel))
		if err != nil {
			t.Errorf("expected %s in new dir: %v", rel, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s content mismatch: got %q, want %q", rel, string(got), want)
		}
	}

	// Old dir must still exist (migration doesn't delete — user
	// is expected to remove it manually once satisfied).
	if _, err := os.Stat(oldDir); err != nil {
		t.Errorf("legacy dir should be left in place, but Stat failed: %v", err)
	}
}

func TestMigrateLegacyConfigDir_SecondCallIsNoOp(t *testing.T) {
	newDir, oldDir := setupIsolatedConfigDirs(t)

	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "config.yaml"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	// First call migrates.
	warn, err := MigrateLegacyConfigDir()
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if warn == "" {
		t.Fatal("first call should produce a warning")
	}
	// Sanity: new dir now exists.
	if _, err := os.Stat(newDir); err != nil {
		t.Fatalf("new dir should exist after first migration: %v", err)
	}

	// Second call sees the new dir already exists and no-ops.
	warn2, err := MigrateLegacyConfigDir()
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if warn2 != "" {
		t.Errorf("second call should be silent no-op, got warning %q", warn2)
	}
}

func TestEnvDeprecations_NoLegacyEnvSet(t *testing.T) {
	for _, name := range []string{
		"KM8__CONFIGPATH", "KBU__CONFIGPATH",
		"KM8__STATEPATH", "KBU__STATEPATH",
		"KM8__ALTERM_SHELL", "KBU__ALTERM_SHELL",
		"KM8__ALTERM_LOGIN_SHELL", "KBU__ALTERM_LOGIN_SHELL",
	} {
		t.Setenv(name, "")
	}
	if got := EnvDeprecations(); len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestEnvDeprecations_FiresForEachLegacyVar(t *testing.T) {
	// Clean slate: unset all KBU__ so KM8__ presence triggers.
	for _, name := range []string{
		"KBU__CONFIGPATH", "KBU__STATEPATH",
		"KBU__ALTERM_SHELL", "KBU__ALTERM_LOGIN_SHELL",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("KM8__CONFIGPATH", "/tmp/c")
	t.Setenv("KM8__STATEPATH", "/tmp/s")
	t.Setenv("KM8__ALTERM_SHELL", "/bin/fish")
	t.Setenv("KM8__ALTERM_LOGIN_SHELL", "true")

	warnings := EnvDeprecations()
	if len(warnings) != 4 {
		t.Fatalf("expected 4 warnings, got %d: %v", len(warnings), warnings)
	}

	joined := strings.Join(warnings, "\n")
	for _, needle := range []string{
		"$KM8__CONFIGPATH", "$KBU__CONFIGPATH",
		"$KM8__STATEPATH", "$KBU__STATEPATH",
		"$KM8__ALTERM_SHELL", "$KBU__ALTERM_SHELL",
		"$KM8__ALTERM_LOGIN_SHELL", "$KBU__ALTERM_LOGIN_SHELL",
	} {
		if !strings.Contains(joined, needle) {
			t.Errorf("warnings should reference %s: got\n%s", needle, joined)
		}
	}
}

func TestEnvDeprecations_KBUSetSuppressesLegacyWarning(t *testing.T) {
	// If the user sets both, they've clearly done the migration for
	// this var — no need to nag about the leftover legacy value.
	t.Setenv("KM8__CONFIGPATH", "/tmp/legacy")
	t.Setenv("KBU__CONFIGPATH", "/tmp/new")
	// Isolate the other three so their state doesn't leak into this
	// assertion.
	t.Setenv("KM8__STATEPATH", "")
	t.Setenv("KM8__ALTERM_SHELL", "")
	t.Setenv("KM8__ALTERM_LOGIN_SHELL", "")

	warnings := EnvDeprecations()
	for _, w := range warnings {
		if strings.Contains(w, "KM8__CONFIGPATH") {
			t.Errorf("expected KM8__CONFIGPATH warning suppressed when KBU__CONFIGPATH is set; got %q", w)
		}
	}
}
