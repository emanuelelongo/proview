package signatures

import (
	"encoding/json"
	"time"
)

// Legacy names are opaque wire data only. Preserve them in existing immutable
// operations so cloud payload hashes remain valid; new saves omit names entirely.
type entryWire struct {
	Version int       `json:"version"`
	ID      string    `json:"id"`
	Name    string    `json:"name,omitempty"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
	Asset   Asset     `json:"asset"`
}

func (e Entry) MarshalJSON() ([]byte, error) {
	return json.Marshal(entryWire{e.Version, e.ID, e.legacyName, e.Created, e.Updated, e.Asset})
}

func (e *Entry) UnmarshalJSON(data []byte) error {
	var wire entryWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*e = Entry{Version: wire.Version, ID: wire.ID, Created: wire.Created, Updated: wire.Updated, Asset: wire.Asset, legacyName: wire.Name}
	return nil
}
