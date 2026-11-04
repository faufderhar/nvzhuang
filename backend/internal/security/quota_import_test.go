package security

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQuotaMergeImportTakesMaxForRecentDays(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	primaryDir, standbyDir := t.TempDir(), t.TempDir()
	primary, err := NewBoltQuotaStore(primaryDir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	standby, err := NewBoltQuotaStore(standbyDir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	today, old := "2026-11-04", "2026-10-01"
	for i := 0; i < 5; i++ {
		_, _ = primary.Incr(quotaStoreKey("alice", today), 100)
	}
	_, _ = primary.Incr(quotaStoreKey("bob", today), 100)
	_, _ = primary.Incr(quotaStoreKey("alice", old), 100)
	for i := 0; i < 3; i++ {
		_, _ = standby.Incr(quotaStoreKey("bob", today), 100)
	}

	// 没有快照时什么都不做
	if n, err := standby.MergeImport(now); n != 0 || err != nil {
		t.Fatalf("no import: %d %v", n, err)
	}
	raw, err := os.ReadFile(primary.Path())
	if err != nil {
		t.Fatal(err)
	}
	importPath := filepath.Join(standbyDir, QuotaImportFile)
	if err := os.WriteFile(importPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := standby.MergeImport(now)
	if err != nil || n != 1 {
		t.Fatalf("merge = %d %v, want 1 nil", n, err)
	}
	if got, _ := standby.Get(quotaStoreKey("alice", today)); got != 5 {
		t.Fatalf("alice = %d want 5", got)
	}
	if got, _ := standby.Get(quotaStoreKey("bob", today)); got != 3 {
		t.Fatalf("bob = %d want 3（取较大值，不能被快照的 1 覆盖）", got)
	}
	if got, _ := standby.Get(quotaStoreKey("alice", old)); got != 0 {
		t.Fatalf("旧日期不应合并, got %d", got)
	}
	if _, err := os.Stat(importPath); !os.IsNotExist(err) {
		t.Fatal("快照合并后应被改名")
	}
	matches, _ := filepath.Glob(importPath + ".merged-*")
	if len(matches) != 1 {
		t.Fatalf("missing merged file: %v", matches)
	}
}

func TestQuotaMergeImportCorruptFileIsQuarantined(t *testing.T) {
	dir := t.TempDir()
	st, err := NewBoltQuotaStore(dir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	importPath := filepath.Join(dir, QuotaImportFile)
	if err := os.WriteFile(importPath, []byte("not a bolt file at all, definitely corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MergeImport(time.Now()); err == nil {
		t.Fatal("expected error for corrupt import")
	}
	if matches, _ := filepath.Glob(importPath + ".bad-*"); len(matches) != 1 {
		t.Fatalf("corrupt import should be quarantined: %v", matches)
	}
	if err := st.Ping(); err != nil {
		t.Fatalf("local store must stay usable: %v", err)
	}
}
