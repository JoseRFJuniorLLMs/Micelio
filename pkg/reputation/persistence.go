package reputation

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// persistedData is the JSON-serializable format for trust store persistence.
type persistedData struct {
	Peers     map[string]*TrustRecord `json:"peers"`
	UpdatedAt string                  `json:"updated_at"`
}

// Save serializes the TrustStore to a JSON file at the given path.
// The file format is: {"peers": {"did:key:z6Mk...": {...}, ...}, "updated_at": "..."}
func (ts *TrustStore) Save(path string) error {
	records := ts.Records()

	data := persistedData{
		Peers:     records,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("reputation: marshal trust store: %w", err)
	}

	if err := os.WriteFile(path, raw, 0644); err != nil {
		return fmt.Errorf("reputation: write trust store to %s: %w", path, err)
	}

	return nil
}

// Load reads a TrustStore from a JSON file at the given path.
// Returns a new TrustStore populated with the persisted records.
func Load(path string) (*TrustStore, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reputation: read trust store from %s: %w", path, err)
	}

	var data persistedData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("reputation: unmarshal trust store: %w", err)
	}

	ts := NewTrustStore()
	if data.Peers != nil {
		ts.SetRecords(data.Peers)
	}

	return ts, nil
}
