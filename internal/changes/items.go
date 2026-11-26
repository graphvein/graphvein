package changes

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// Page limits of Items (ADR 0056).
const (
	DefaultLimit = 100
	MaxLimit     = 500
)

// Filter selects items. Empty fields select everything; test-scope items
// are left out unless IncludeTests is set (G7), and counted in Hidden.
type Filter struct {
	Group, Change, Cause, Kind string
	// Q matches a substring of the identity or the name.
	Q            string
	IncludeTests bool
	// Raw adds both stored payloads to every item.
	Raw bool
}

func (f Filter) key() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{f.Group, f.Change, f.Cause, f.Kind, f.Q, fmt.Sprint(f.IncludeTests)}, "\x00")))
	return hex.EncodeToString(sum[:8])
}

func (f Filter) match(it *Item) bool {
	return (f.Group == "" || it.Group == f.Group) && (f.Change == "" || it.Change == f.Change) &&
		(f.Cause == "" || it.Cause != nil && *it.Cause == f.Cause) && (f.Kind == "" || it.Kind == f.Kind) &&
		(f.Q == "" || strings.Contains(it.Identity, f.Q) || strings.Contains(it.Name, f.Q))
}

// Page is one request's position: an opaque cursor from the previous page
// (empty for the first) and a limit (0 = DefaultLimit).
type Page struct {
	Cursor string
	Limit  int
}

// ItemPage is one page of items.
type ItemPage struct {
	Items []Item `json:"items"`
	// Total counts the matching items, Hidden the matching test-scope
	// items left out (G7).
	Total  int     `json:"total"`
	Hidden int     `json:"hidden_tests"`
	Next   *string `json:"next_cursor"`
}

type cursor struct {
	C string `json:"c"`
	F string `json:"f"`
	O int    `json:"o"`
}

// Items returns one page of the comparison's items in their stable order.
// A cursor from another comparison or filter is refused with
// ErrCursorMismatch (G6).
func (c *Comparison) Items(f Filter, p Page) (ItemPage, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		return ItemPage{}, fmt.Errorf("limit %d is above the maximum %d", limit, MaxLimit)
	}
	offset := 0
	if p.Cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(p.Cursor)
		var cur cursor
		if err != nil || json.Unmarshal(data, &cur) != nil || cur.O < 0 {
			return ItemPage{}, ErrCursorInvalid
		}
		if cur.C != c.ID() || cur.F != f.key() {
			return ItemPage{}, ErrCursorMismatch
		}
		offset = cur.O
	}
	out := ItemPage{Items: []Item{}}
	for i := range c.items {
		it := &c.items[i]
		if !f.match(it) {
			continue
		}
		if it.Test && !f.IncludeTests {
			out.Hidden++
			continue
		}
		if out.Total >= offset && len(out.Items) < limit {
			out.Items = append(out.Items, c.present(it, f.Raw))
		}
		out.Total++
	}
	if offset+len(out.Items) < out.Total {
		data, _ := json.Marshal(cursor{C: c.ID(), F: f.key(), O: offset + len(out.Items)})
		next := base64.RawURLEncoding.EncodeToString(data)
		out.Next = &next
	}
	return out, nil
}

// Item returns one item by id ("<record>:<key>", or a node identity).
func (c *Comparison) Item(id string, raw bool) (Item, bool) {
	i, ok := c.byID[id]
	if !ok {
		return Item{}, false
	}
	return c.present(&c.items[i], raw), true
}

// AllItems returns every item, test scope included, in the stable order.
func (c *Comparison) AllItems() []Item {
	out := make([]Item, len(c.items))
	for i := range c.items {
		out[i] = c.present(&c.items[i], false)
	}
	return out
}

func (c *Comparison) present(it *Item, raw bool) Item {
	out := *it
	c.mu.Lock()
	r, ok := c.impacts[it.ID()]
	c.mu.Unlock()
	if ok {
		n := len(r.Paths)
		out.ImpactCount = &n
	}
	if raw && out.Raw == nil {
		r := it.raw
		out.Raw = &r
	}
	return out
}
