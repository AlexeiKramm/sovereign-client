package ui

import (
	"path/filepath"
	"testing"
)

func TestHistoryStoreAddAndCap(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "history.json")
	hs := NewHistoryStore(tmp)

	for i := 1; i <= 10; i++ {
		hs.Add("clean utterance number", "raw utterance number", int64(i*10), int64(i*5))
	}

	items := hs.Items()
	if len(items) != HistoryMaxItems {
		t.Fatalf("expected %d items, got %d", HistoryMaxItems, len(items))
	}

	// First item should be the latest (number 10)
	if items[0].AsrMs != 100 {
		t.Errorf("latest item ASR = %d, want 100", items[0].AsrMs)
	}
	if items[0].Words != 3 {
		t.Errorf("word count = %d, want 3", items[0].Words)
	}

	// Persistence reload
	hs2 := NewHistoryStore(tmp)
	if len(hs2.Items()) != HistoryMaxItems {
		t.Fatalf("reloaded store items = %d, want %d", len(hs2.Items()), HistoryMaxItems)
	}
}

func TestHistoryStoreViewMode(t *testing.T) {
	hs := NewHistoryStore("")
	item := hs.Add("Clean text", "Raw text", 100, 20)

	if item.ViewMode != ViewClean {
		t.Fatalf("initial view mode = %q, want %q", item.ViewMode, ViewClean)
	}

	hs.SetViewMode(item.ID, ViewDiff)
	items := hs.Items()
	if items[0].ViewMode != ViewDiff {
		t.Fatalf("updated view mode = %q, want %q", items[0].ViewMode, ViewDiff)
	}
}
