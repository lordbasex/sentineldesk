// SentinelDesk
// A collaborative operating system for people and AI agents.
//
// Copyright 2026 Federico Pereira <fpereira@cnsoluciones.com>
// Co-authored by Nicolas Pereira <npereira@cnsoluciones.com>
//
// Licensed under the Apache License, Version 2.0.
//
// This product's name and logo are trademarks of Federico Pereira and are not
// covered by the license above. See the README for the trademark policy.
//
// SPDX-License-Identifier: Apache-2.0

package store

// Turning tokens into money.
//
// Tokens are recorded; money is derived. That ordering matters and is not
// pedantry: a price changes, and a price stored beside each row would then be
// wrong for every past run with no way to notice. Recomputing from a rate the
// operator controls keeps the history correct when the rate moves.
//
// The rates below are DEFAULTS AND ESTIMATES. They are here so `costs` prints
// something useful on the first run, not because this file is an authority on
// anybody's billing. The operator's own invoice is the authority, and
// ~/.sentineldesk/pricing.json overrides these — which is also the honest way
// to handle the fact that this file cannot know about a model released after it
// was written.
//
// Calibrating is arithmetic: take the token counts `costs` reports, take the
// amount the console says was spent, and adjust.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Rate is dollars per million tokens.
//
// CacheWrite and CacheRead are multipliers on Input, not absolute prices,
// because that is how every provider that offers caching expresses it and
// because a multiplier survives a change to the base rate. Zero means "use the
// default multiplier"; the defaults are estimates like everything else here.
type Rate struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheWrite float64 `json:"cache_write_multiplier,omitempty"`
	CacheRead  float64 `json:"cache_read_multiplier,omitempty"`
}

// Writing to a cache costs a little more than a plain input token; reading from
// one costs much less. Both are estimates and both are overridable.
const (
	defaultCacheWriteMultiplier = 1.25
	defaultCacheReadMultiplier  = 0.10
)

// defaultRates is keyed by a substring of the model id, longest match first, so
// a new point release inherits its family's rate instead of falling to zero and
// silently reporting that everything was free.
var defaultRates = map[string]Rate{
	"opus":   {Input: 15.00, Output: 75.00},
	"sonnet": {Input: 3.00, Output: 15.00},
	"haiku":  {Input: 1.00, Output: 5.00},
	"fable":  {Input: 3.00, Output: 15.00},
}

// Pricing resolves a model id to a rate.
type Pricing struct {
	rates  map[string]Rate
	source string
}

// LoadPricing reads ~/.sentineldesk/pricing.json when it exists.
func LoadPricing() Pricing {
	p := Pricing{rates: map[string]Rate{}, source: "built-in estimates"}
	for k, v := range defaultRates {
		p.rates[k] = v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	path := filepath.Join(home, ".sentineldesk", "pricing.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return p
	}
	var override map[string]Rate
	if json.Unmarshal(raw, &override) != nil {
		return p
	}
	for k, v := range override {
		p.rates[strings.ToLower(k)] = v
	}
	p.source = path
	return p
}

// Source says where the rates came from, so a number is never presented as
// more certain than it is.
func (p Pricing) Source() string { return p.source }

// Estimated returns the estimated dollar cost, and whether a rate was found.
//
// The second return is not decoration. An unknown model priced at zero would
// report a day's work as free, which is the worst possible way to be wrong
// about a bill.
func (p Pricing) Estimated(model string, in, out int) (float64, bool) {
	return p.EstimatedWithCache(model, in, out, 0, 0)
}

// EstimatedWithCache prices a turn including whatever caching did to it.
func (p Pricing) EstimatedWithCache(model string, in, out, cacheWrite, cacheRead int) (float64, bool) {
	model = strings.ToLower(model)
	best, bestLen, found := Rate{}, 0, false
	for key, rate := range p.rates {
		if strings.Contains(model, key) && len(key) > bestLen {
			best, bestLen, found = rate, len(key), true
		}
	}
	if !found {
		return 0, false
	}
	write := best.CacheWrite
	if write == 0 {
		write = defaultCacheWriteMultiplier
	}
	read := best.CacheRead
	if read == 0 {
		read = defaultCacheReadMultiplier
	}
	total := float64(in)/1e6*best.Input +
		float64(out)/1e6*best.Output +
		float64(cacheWrite)/1e6*best.Input*write +
		float64(cacheRead)/1e6*best.Input*read
	return total, true
}
