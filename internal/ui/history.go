package ui

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alexeikramm/sovereign-client/internal/core"
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

// historyCipher seals and opens the persisted history. Production uses the per-user OS secret store
// (DPAPI on Windows); tests inject a reversible fake so the store logic runs on any OS.
type historyCipher interface {
	seal(plain []byte) ([]byte, error)
	open(blob []byte) ([]byte, error)
}

// osHistoryCipher is the production cipher: the same per-user OS secret store the credential file
// uses, so transcripts are never written to disk in the clear.
type osHistoryCipher struct{}

func (osHistoryCipher) seal(plain []byte) ([]byte, error) { return core.ProtectAtRest(plain) }
func (osHistoryCipher) open(blob []byte) ([]byte, error)  { return core.UnprotectAtRest(blob) }

// HistoryStore manages recent dictations with thread-safety and OS-encrypted persistence.
type HistoryStore struct {
	mu     sync.Mutex
	items  []HistoryItem
	path   string
	cipher historyCipher
}

// NewHistoryStore creates an empty history store encrypted with the OS secret store.
func NewHistoryStore(path string) *HistoryStore {
	return newHistoryStore(path, osHistoryCipher{})
}

// newHistoryStore is the seam tests use to supply a reversible cipher.
func newHistoryStore(path string, cipher historyCipher) *HistoryStore {
	hs := &HistoryStore{path: path, cipher: cipher}
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
	if h.path == "" || h.cipher == nil {
		return
	}
	dir := filepath.Dir(h.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}
	data, err := json.Marshal(h.items)
	if err != nil {
		return
	}
	blob, err := h.cipher.seal(data)
	if err != nil {
		// Refuse to fall back to plaintext: without an OS secret store the history is not persisted
		// (the in-memory list still works for this run).
		return
	}
	tmp := h.path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0600); err == nil {
		_ = os.Rename(tmp, h.path)
	}
}

// Load reads and decrypts persisted history from disk. A missing file is not an error. A legacy
// plaintext file (pre-encryption) is imported so no transcript is lost, and is re-sealed on the
// next save.
func (h *HistoryStore) Load() error {
	if h.path == "" || h.cipher == nil {
		return nil
	}
	blob, err := os.ReadFile(h.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	legacy := false
	data, err := h.cipher.open(blob)
	if err != nil {
		if !looksLikeJSON(blob) {
			return err
		}
		legacy = true
		data = blob
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
	if legacy {
		// Re-seal the pre-encryption file at once so the plaintext does not linger on disk.
		h.saveLocked()
	}
	return nil
}

// looksLikeJSON reports whether b starts (after whitespace) with a JSON container, identifying a
// legacy plaintext history file from before it was encrypted.
func looksLikeJSON(b []byte) bool {
	for _, c := range b {
		switch c {
		case ' ', '\t', '\r', '\n':
			continue
		case '[', '{':
			return true
		default:
			return false
		}
	}
	return false
}

func countWords(s string) int {
	return len(strings.Fields(s))
}
