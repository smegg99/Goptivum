// core/lockout/lockout.go

// Package lockout counts failed sign-ins per sign-in name and per client
// address and blocks either one for a while once it reaches its limit.
//
// Kratos v1.3.1 has no lockout of its own, so the server's /kratos proxy asks
// this before it forwards a sign-in and tells it how the sign-in ended. The
// counts live in memory only: a restart clears them.
package lockout

import (
	"strings"
	"sync"
	"time"
)

// The ranges settings.cue and the contract allow.
const (
	MaxAccountFailures = 100
	MaxAddressFailures = 1000
	MaxBlock           = 1440 * time.Minute
)

// MaxTracked bounds each table, so a flood of made-up names cannot exhaust
// memory. At the bound the entry that started counting first is dropped.
const MaxTracked = 10000

// Limits are what an administrator sets.
type Limits struct {
	// AccountFailures is how many failed sign-ins with one name block it.
	AccountFailures int
	// AddressFailures is how many failed sign-ins from one address block it.
	AddressFailures int
	// Block is how long a block lasts, and how far back failures count.
	Block time.Duration
}

// Invalid names the first limit outside its range, in the contract's field
// names, or returns an empty string when every limit is usable.
func (l Limits) Invalid() string {
	switch {
	case l.AccountFailures < 1 || l.AccountFailures > MaxAccountFailures:
		return "account_failures"
	case l.AddressFailures < 1 || l.AddressFailures > MaxAddressFailures:
		return "address_failures"
	case l.Block < time.Minute || l.Block > MaxBlock:
		return "block_minutes"
	}
	return ""
}

// Block says what a failure blocked.
type Block struct {
	// Account and Address report which limit this failure reached.
	Account bool
	Address bool
	// For is how long the block lasts.
	For time.Duration
}

// count is one name's or one address's failures.
type count struct {
	failures int
	// since is when the first counted failure happened.
	since time.Time
	// until is when a block ends; zero when there is none.
	until time.Time
}

// Guard holds the counts. It is safe for concurrent use.
type Guard struct {
	now func() time.Time

	mu        sync.Mutex
	limits    Limits
	accounts  map[string]*count
	addresses map[string]*count
}

// New builds a guard. now is the clock, time.Now outside tests.
func New(limits Limits, now func() time.Time) *Guard {
	if now == nil {
		now = time.Now
	}
	return &Guard{now: now, limits: limits,
		accounts: map[string]*count{}, addresses: map[string]*count{}}
}

// SetLimits replaces the limits. A block already running keeps its end.
func (g *Guard) SetLimits(limits Limits) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.limits = limits
}

// Limits are the limits in force.
func (g *Guard) Limits() Limits {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.limits
}

// Blocked is how long the name or the address stays blocked, the longer of
// the two, and zero when neither is.
func (g *Guard) Blocked(account, address string) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	left := remaining(g.accounts[normalize(account)], now)
	if byAddress := remaining(g.addresses[address], now); byAddress > left {
		left = byAddress
	}
	return left
}

// Failed counts one failed sign-in against the name and the address.
func (g *Guard) Failed(account, address string) Block {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	block := Block{For: g.limits.Block}
	if name := normalize(account); name != "" {
		block.Account = g.fail(g.accounts, name, g.limits.AccountFailures, now)
	}
	if address != "" {
		block.Address = g.fail(g.addresses, address, g.limits.AddressFailures, now)
	}
	return block
}

// Succeeded clears the name's count. The address keeps its own.
func (g *Guard) Succeeded(account string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.accounts, normalize(account))
}

// Tracked is how many names and addresses are counted right now.
func (g *Guard) Tracked() (accounts, addresses int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.accounts), len(g.addresses)
}

// fail adds one failure and reports whether it started a block.
func (g *Guard) fail(table map[string]*count, key string, limit int, now time.Time) bool {
	entry, ok := table[key]
	if !ok {
		g.makeRoom(table, now)
		entry = &count{}
		table[key] = entry
	}
	if entry.until.After(now) {
		return false
	}
	if entry.failures == 0 || now.Sub(entry.since) >= g.limits.Block {
		entry.failures, entry.since = 0, now
	}
	entry.failures++
	if entry.failures < limit {
		return false
	}
	entry.failures, entry.since, entry.until = 0, time.Time{}, now.Add(g.limits.Block)
	return true
}

// makeRoom drops what has run out, and at the bound the oldest entry.
func (g *Guard) makeRoom(table map[string]*count, now time.Time) {
	if len(table) < MaxTracked {
		return
	}
	oldestKey, oldest := "", time.Time{}
	for key, entry := range table {
		if !entry.until.After(now) && (entry.failures == 0 || now.Sub(entry.since) >= g.limits.Block) {
			delete(table, key)
			continue
		}
		started := entry.since
		if started.IsZero() {
			started = entry.until
		}
		if oldestKey == "" || started.Before(oldest) {
			oldestKey, oldest = key, started
		}
	}
	if len(table) >= MaxTracked && oldestKey != "" {
		delete(table, oldestKey)
	}
}

func remaining(entry *count, now time.Time) time.Duration {
	if entry == nil || !entry.until.After(now) {
		return 0
	}
	return entry.until.Sub(now)
}

// normalize compares names the way Kratos does: case and surrounding spaces
// do not make a new name.
func normalize(account string) string {
	return strings.ToLower(strings.TrimSpace(account))
}
