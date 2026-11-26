package server

import (
	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/store"
)

// settingsNotIndexed is the settings state without runs.
const settingsNotIndexed = "not_indexed"

// settingsStatus compares the graphvein.yaml settings the last index run
// read with the current file (ADR 0088, rows ST-41..ST-46 of
// docs/contract/claims/settings.md): freshness.settings of /api/v0/status
// and of each project member, index_settings of /api/v0/config. It never
// changes freshness.state, which describes the files.
type settingsStatus struct {
	// State is unchanged, changed, unknown or not_indexed. Clients treat
	// any other value as unknown.
	State string `json:"state"`
	// ChangedKeys are the keys whose value differs, in key-table order.
	ChangedKeys []string `json:"changed_keys"`
	// UnknownKeys are current keys the run's record does not hold.
	UnknownKeys []string `json:"unknown_keys"`
	IndexHash   *string  `json:"index_hash"`
	CurrentHash *string  `json:"current_hash"`
	// Reason says why the state is unknown; null otherwise.
	Reason *string `json:"reason"`
}

// settingsStatusFor compares the latest run's record with compare's
// current settings; not_indexed without a usable run.
func settingsStatusFor(runs []store.Run, compare func(record string) config.IndexSettings) *settingsStatus {
	out := &settingsStatus{State: settingsNotIndexed, ChangedKeys: []string{}, UnknownKeys: []string{}}
	run := latestStoreRun(runs)
	if run == nil {
		return out
	}
	c := compare(run.Settings)
	out.State = c.State
	out.ChangedKeys = append(out.ChangedKeys, c.ChangedKeys...)
	out.UnknownKeys = append(out.UnknownKeys, c.UnknownKeys...)
	if c.IndexHash != "" {
		out.IndexHash = &c.IndexHash
	}
	if c.CurrentHash != "" {
		out.CurrentHash = &c.CurrentHash
	}
	if c.Reason != "" {
		out.Reason = &c.Reason
	}
	return out
}

// noCheckoutSettings is the comparison of a member without a checkout:
// its current file is not here, so nothing is proven.
func noCheckoutSettings(record string) config.IndexSettings {
	return config.UnknownIndexSettings(record, "no checkout here: the settings comparison needs the member's "+config.File)
}

// settingsAt compares with root's graphvein.yaml as it is now.
func settingsAt(root string) func(string) config.IndexSettings {
	return func(record string) config.IndexSettings { return config.IndexSettingsAt(record, root) }
}

// statusSettings is the comparison of the options' repository.
func (o Options) statusSettings(runs []store.Run) *settingsStatus {
	if o.noCheckout {
		return settingsStatusFor(runs, noCheckoutSettings)
	}
	return settingsStatusFor(runs, settingsAt(o.Root))
}
