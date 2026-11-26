package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
)

// This file holds the index inputs of graphvein.yaml (ADR 0088, rows
// ST-41..ST-45 of docs/contract/claims/settings.md): the effective values
// of the keys an index run reads, in a canonical form, so a run can record
// what it was built with and freshness can say "settings changed since this
// index" naming the keys. The keys are the next_index_run rows of the
// Settings key table; at-once keys (ui.*, ide.editor, architecture.hide)
// are read on every request and never make an index stale.

// indexInputsFormat versions the canonical form. A record of another
// format is compared as unknown, never as unchanged or changed.
const indexInputsFormat = 1

// States of the settings an index was built with, compared with the
// current graphvein.yaml.
const (
	// IndexSettingsUnchanged: every index input has the value the run used.
	IndexSettingsUnchanged = "unchanged"
	// IndexSettingsChanged: at least one index input differs.
	IndexSettingsChanged = "changed"
	// IndexSettingsUnknown: the comparison proves nothing (the run did not
	// record its settings, or the current file does not load).
	IndexSettingsUnknown = "unknown"
)

// IndexInputs is the canonical form of the graphvein.yaml keys an index run
// reads: each key's effective value, the built-in default when the file
// does not set it. Lists are sorted without duplicates and maps are keyed
// objects, because the index reads patterns as a set (any match counts)
// and base paths as a Go map: order, duplicates, comments, quoting and
// layout change nothing the index produces, so they never change the
// inputs either.
type IndexInputs struct {
	values map[string]any
}

// IndexInputKeys lists the index input keys in table order: the Settings
// keys that apply at the next index run.
func IndexInputKeys() []string {
	var keys []string
	for _, k := range settingKeys {
		if k.Applies == AppliesNextIndexRun {
			keys = append(keys, k.Key)
		}
	}
	return keys
}

// IndexInputsOf reads the index inputs of graphvein.yaml text (nil or
// empty: no file). The caller has validated data with Parse.
func IndexInputsOf(data []byte) (IndexInputs, error) {
	values, _, err := SettingValues(data)
	if err != nil {
		return IndexInputs{}, err
	}
	in := IndexInputs{values: map[string]any{}}
	for _, v := range values {
		if v.Key.Applies != AppliesNextIndexRun {
			continue
		}
		in.values[v.Key.Key] = canonicalInput(v.Key, v.Value)
	}
	return in, nil
}

// canonicalInput is the order-free form of one effective value.
func canonicalInput(k SettingKey, value any) any {
	switch k.Kind {
	case SettingList:
		items, _ := value.([]string)
		out := append([]string{}, items...)
		sort.Strings(out)
		return slices.Compact(out)
	case SettingMap:
		pairs, _ := value.([]MapEntry)
		out := map[string]string{}
		for _, p := range pairs {
			out[p.Key] = p.Value
		}
		return out
	}
	return value
}

// LoadIndexInputs reads root's graphvein.yaml once and returns the
// configuration and its index inputs from the same bytes, so an index run
// records exactly the settings it used. A missing file gives the defaults.
func LoadIndexInputs(root string) (Config, IndexInputs, error) {
	path := filepath.Join(root, File)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		data, err = nil, nil
	}
	if err != nil {
		return Config{}, IndexInputs{}, fmt.Errorf("reading %s: %w", path, err)
	}
	c := Defaults()
	if data != nil {
		if c, err = Parse(path, data); err != nil {
			return Config{}, IndexInputs{}, err
		}
	}
	in, err := IndexInputsOf(data)
	if err != nil {
		return Config{}, IndexInputs{}, fmt.Errorf("reading the index settings of %s: %w", path, err)
	}
	return c, in, nil
}

// WithShapeDepth records the shape depth a run used when a flag (graphvein
// index --shape-depth) overrode the file's.
func (in IndexInputs) WithShapeDepth(depth int) IndexInputs {
	out := IndexInputs{values: map[string]any{}}
	for k, v := range in.values {
		out.values[k] = v
	}
	out.values["index.shape_depth"] = depth
	return out
}

// IsZero is true for inputs that were never read (an index run whose
// emitter did not report them).
func (in IndexInputs) IsZero() bool { return in.values == nil }

// Hash is "sha256:" and the hex digest of the canonical JSON of the values.
func (in IndexInputs) Hash() string {
	data, _ := json.Marshal(in.values)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// indexInputsRecord is the document an index run stores.
type indexInputsRecord struct {
	Format int                        `json:"format"`
	Hash   string                     `json:"hash"`
	Values map[string]json.RawMessage `json:"values"`
}

// Record is the document an index run stores with its run: the format,
// the hash and every key's canonical value. Empty for zero inputs.
func (in IndexInputs) Record() string {
	if in.IsZero() {
		return ""
	}
	values := map[string]json.RawMessage{}
	for k, v := range in.values {
		data, _ := json.Marshal(v)
		values[k] = data
	}
	data, _ := json.Marshal(indexInputsRecord{Format: indexInputsFormat, Hash: in.Hash(), Values: values})
	return string(data)
}

// IndexSettings compares the settings an index run recorded with the
// current ones. Every field but State may be empty.
type IndexSettings struct {
	State string
	// ChangedKeys are the index input keys whose value differs, in table
	// order.
	ChangedKeys []string
	// UnknownKeys are current index input keys the record does not hold
	// (a key added after the run): their value at the run is not known.
	UnknownKeys []string
	// IndexHash is the recorded hash; CurrentHash the current one.
	IndexHash, CurrentHash string
	// Reason says why the state is unknown.
	Reason string
}

// CompareIndexInputs compares a run's record (empty: not recorded) with
// the current inputs. It never reports unchanged without proof: a missing
// or unreadable record, another format, or a key the record lacks makes
// the answer unknown unless another key has provably changed.
func CompareIndexInputs(record string, current IndexInputs) IndexSettings {
	out := IndexSettings{State: IndexSettingsUnknown}
	if !current.IsZero() {
		out.CurrentHash = current.Hash()
	}
	if record == "" {
		out.Reason = "the index run did not record its settings (an index from before settings were recorded, or from an external indexer)"
		return out
	}
	var r indexInputsRecord
	if err := json.Unmarshal([]byte(record), &r); err != nil {
		out.Reason = "the recorded settings do not decode: " + err.Error()
		return out
	}
	if r.Format != indexInputsFormat {
		out.Reason = fmt.Sprintf("the settings were recorded in format %d; this version reads format %d", r.Format, indexInputsFormat)
		return out
	}
	out.IndexHash = r.Hash
	if current.IsZero() {
		out.Reason = "the current settings are not known"
		return out
	}
	for _, key := range IndexInputKeys() {
		stored, ok := r.Values[key]
		if !ok {
			out.UnknownKeys = append(out.UnknownKeys, key)
			continue
		}
		now, _ := json.Marshal(current.values[key])
		var compact bytes.Buffer
		if err := json.Compact(&compact, stored); err != nil || !bytes.Equal(compact.Bytes(), now) {
			out.ChangedKeys = append(out.ChangedKeys, key)
		}
	}
	switch {
	case len(out.ChangedKeys) > 0:
		out.State = IndexSettingsChanged
	case len(out.UnknownKeys) > 0:
		out.Reason = "the index run did not record these keys"
	default:
		out.State = IndexSettingsUnchanged
	}
	return out
}

// IndexSettingsAt compares a run's record with root's graphvein.yaml as it
// is now. A file that does not load (or cannot be read) is unknown with
// the decoder's message.
func IndexSettingsAt(record, root string) IndexSettings {
	path := filepath.Join(root, File)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		data, err = nil, nil
	}
	if err == nil && data != nil {
		// The file's own name, not its absolute path, so an answer
		// never carries the checkout's location.
		_, err = Parse(File, data)
	}
	var current IndexInputs
	if err == nil {
		current, err = IndexInputsOf(data)
	}
	if err != nil {
		return UnknownIndexSettings(record, "the current "+File+" does not load: "+err.Error())
	}
	return CompareIndexInputs(record, current)
}

// UnknownIndexSettings is the comparison when the current settings are not
// known (the file does not load, or it is not here), with the record's
// hash when it has one.
func UnknownIndexSettings(record, reason string) IndexSettings {
	out := CompareIndexInputs(record, IndexInputs{})
	out.State, out.ChangedKeys, out.UnknownKeys, out.Reason = IndexSettingsUnknown, nil, nil, reason
	return out
}
