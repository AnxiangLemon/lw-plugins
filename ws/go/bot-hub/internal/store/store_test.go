package store

import (
	"path/filepath"
	"testing"
)

func TestClearLogs(t *testing.T) {
	dir := t.TempDir()
	st := New(dir)
	st.AddLog(LogEntry{Category: "system", Summary: "sys-1"})
	st.AddLog(LogEntry{Category: "monitor", Summary: "mon-1"})
	st.AddLog(LogEntry{Category: "system", Summary: "sys-2"})

	if got := len(st.Logs("all", 50)); got != 3 {
		t.Fatalf("expected 3 logs, got %d", got)
	}

	if err := st.ClearLogs("system"); err != nil {
		t.Fatalf("clear system logs failed: %v", err)
	}
	if got := len(st.Logs("all", 50)); got != 1 {
		t.Fatalf("expected 1 remaining log after clearing system, got %d", got)
	}
	if st.Logs("all", 50)[0].Category != "monitor" {
		t.Fatalf("expected remaining log to be monitor")
	}

	// 验证磁盘持久化同步生效
	reloaded := New(filepath.Clean(dir))
	if got := len(reloaded.Logs("all", 50)); got != 1 {
		t.Fatalf("expected 1 persisted log, got %d", got)
	}

	if err := st.ClearLogs("all"); err != nil {
		t.Fatalf("clear all logs failed: %v", err)
	}
	if got := len(st.Logs("all", 50)); got != 0 {
		t.Fatalf("expected 0 logs after clearing all, got %d", got)
	}

	reloaded2 := New(filepath.Clean(dir))
	if got := len(reloaded2.Logs("all", 50)); got != 0 {
		t.Fatalf("expected 0 persisted logs after clearing all, got %d", got)
	}
}
