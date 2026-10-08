package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// HistoryMaxItems is the maximum number of items retained in local history (last 5).
const HistoryMaxItems = 5

// HistoryViewMode represents which textual representation is active on the history card.
const (
	ViewClean = "clean"
	ViewRaw   = "raw"
	ViewDiff  = "diff"
)

// HistoryItem represents one completed dictation session recorded on the client.
type HistoryItem struct {
	ID        string    `json:"id"`
	Time      time.Time `json:"time"`
	CleanText string    `json:"clean_text"`
	RawText   string    `json:"raw_text"`
	AsrMs     int64     `json:"asr_ms"`
	CleanupMs int64     `json:"cleanup_ms"`
	Words     int       `json:"words"`
	ViewMode  string    `json:"view_mode"` // "clean", "raw", or "diff"
}

// HistoryStore manages recent dictations with thread-safety and optional JSON persistence.
type HistoryStore struct {
	mu    sync.Mutex
	items []HistoryItem
	path  string
}

// NewHistoryStore creates an empty history store.
func NewHistoryStore(path string) *HistoryStore {
	hs := &HistoryStore{path: path}
	if path != "" {
		_ = hs.Load()
	}
	return hs
}

// Add appends a newly completed utterance at the front (most recent first), capping at HistoryMaxItems.
func (h *HistoryStore) Add(clean, raw string, asrMs, cleanupMs int64) HistoryItem {
	clean = strings.TrimSpace(clean)
	raw = strings.TrimSpace(raw)
	words := countWords(clean)
	if words == 0 && clean != "" {
		words = 1
	}

	item := HistoryItem{
		ID:        time.Now().Format("20060102-150405.000"),
		Time:      time.Now(),
		CleanText: clean,
		RawText:   raw,
		AsrMs:     asrMs,
		CleanupMs: cleanupMs,
		Words:     words,
		ViewMode:  ViewClean,
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Prepend new item
	h.items = append([]HistoryItem{item}, h.items...)
	if len(h.items) > HistoryMaxItems {
		h.items = h.items[:HistoryMaxItems]
	}

	h.saveLocked()
	return item
}

// Items returns a copy of current history items in newest-to-oldest order.
func (h *HistoryStore) Items() []HistoryItem {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]HistoryItem, len(h.items))
	copy(out, h.items)
	return out
}

// SetViewMode updates the view mode ("clean", "raw", "diff") for a specific history item.
func (h *HistoryStore) SetViewMode(id, mode string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.items {
		if h.items[i].ID == id {
			h.items[i].ViewMode = mode
			h.saveLocked()
			return
		}
	}
}

// Clear empties the local history.
func (h *HistoryStore) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.items = nil
	h.saveLocked()
}

func (h *HistoryStore) saveLocked() {
	if h.path == "" {
		return
	}
	dir := filepath.Dir(h.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}
	data, err := json.MarshalIndent(h.items, "", "  ")
	if err != nil {
		return
	}
	tmp := h.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err == nil {
		_ = os.Rename(tmp, h.path)
	}
}

// Load reads persisted history from disk.
func (h *HistoryStore) Load() error {
	if h.path == "" {
		return nil
	}
	data, err := os.ReadFile(h.path)
	if err != nil {
		return err
	}
	var loaded []HistoryItem
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(loaded) > HistoryMaxItems {
		loaded = loaded[:HistoryMaxItems]
	}
	h.items = loaded
	return nil
}

func countWords(s string) int {
	return len(strings.Fields(s))
}
