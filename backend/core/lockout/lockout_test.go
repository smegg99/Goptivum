// core/lockout/lockout_test.go

package lockout_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/smegg99/goptivum/backend/core/lockout"
)

// clock is a hand-moved time, so no test sleeps.
type clock struct{ at time.Time }

func (c *clock) now() time.Time             { return c.at }
func (c *clock) advance(step time.Duration) { c.at = c.at.Add(step) }

func newGuard(t *testing.T, limits lockout.Limits) (*lockout.Guard, *clock) {
	t.Helper()
	at := &clock{at: time.Date(2026, 9, 27, 8, 0, 0, 0, time.UTC)}
	return lockout.New(limits, at.now), at
}

var school = lockout.Limits{AccountFailures: 3, AddressFailures: 5, Block: 15 * time.Minute}

func TestANameIsBlockedAfterItsLimitAndFreedAfterTheBlock(t *testing.T) {
	guard, at := newGuard(t, school)

	for range 2 {
		guard.Failed("anna", "10.0.0.1")
	}
	if left := guard.Blocked("anna", "10.0.0.1"); left != 0 {
		t.Fatalf("blocked after 2 of 3 failures: %v", left)
	}
	block := guard.Failed("anna", "10.0.0.1")
	if !block.Account || block.Address {
		t.Errorf("third failure = %+v, want the name blocked and not the address", block)
	}
	if left := guard.Blocked("anna", "10.0.0.2"); left != 15*time.Minute {
		t.Errorf("left from another address = %v, want the whole block", left)
	}

	at.advance(10 * time.Minute)
	if left := guard.Blocked("anna", "10.0.0.2"); left != 5*time.Minute {
		t.Errorf("left after 10 minutes = %v, want 5m", left)
	}
	at.advance(5 * time.Minute)
	if left := guard.Blocked("anna", "10.0.0.2"); left != 0 {
		t.Errorf("still blocked when the block ran out: %v", left)
	}
}

// A name no account has is counted like one that exists: the guard never
// asks, so the block cannot tell a stranger which names are real.
func TestTheNameIsComparedIgnoringCaseAndSpaces(t *testing.T) {
	guard, _ := newGuard(t, school)
	guard.Failed("Anna@Example.invalid", "10.0.0.1")
	guard.Failed("  anna@example.invalid ", "10.0.0.2")
	guard.Failed("ANNA@EXAMPLE.INVALID", "10.0.0.3")
	if guard.Blocked(" Anna@example.INVALID", "10.0.0.4") == 0 {
		t.Error("three spellings of one name were counted as three names")
	}
}

func TestAnAddressIsBlockedAfterItsLimitWhateverTheNames(t *testing.T) {
	guard, _ := newGuard(t, school)
	for index := range 5 {
		guard.Failed(fmt.Sprintf("name-%d", index), "10.0.0.9")
	}
	if left := guard.Blocked("somebody-new", "10.0.0.9"); left != 15*time.Minute {
		t.Errorf("a new name from the blocked address = %v, want the block", left)
	}
	if left := guard.Blocked("somebody-new", "10.0.0.10"); left != 0 {
		t.Errorf("another address = %v, want no block", left)
	}
}

// A success clears its name's count, never the address's: an attacker with
// one real account must not be able to reset the address between guesses.
func TestASuccessClearsTheNameOnly(t *testing.T) {
	guard, _ := newGuard(t, school)
	for range 2 {
		guard.Failed("anna", "10.0.0.1")
	}
	for range 2 {
		guard.Failed("piotr", "10.0.0.1")
	}
	guard.Succeeded("anna")
	guard.Failed("anna", "10.0.0.1")
	if left := guard.Blocked("anna", "10.0.0.2"); left != 0 {
		t.Errorf("anna blocked after a success reset her count: %v", left)
	}
	if left := guard.Blocked("nobody", "10.0.0.1"); left != 15*time.Minute {
		t.Errorf("the address = %v, want 5 failures to block it", left)
	}
}

// Failures further apart than the block are not added up, so a person who
// mistypes once a week is never blocked.
func TestOldFailuresAreForgotten(t *testing.T) {
	guard, at := newGuard(t, school)
	guard.Failed("anna", "10.0.0.1")
	guard.Failed("anna", "10.0.0.1")
	at.advance(16 * time.Minute)
	guard.Failed("anna", "10.0.0.1")
	if left := guard.Blocked("anna", "10.0.0.1"); left != 0 {
		t.Errorf("failures 16 minutes apart were added up: %v", left)
	}
}

func TestTheLongerOfTwoBlocksIsReported(t *testing.T) {
	guard, at := newGuard(t, school)
	for index := range 5 {
		guard.Failed(fmt.Sprintf("name-%d", index), "10.0.0.9")
	}
	at.advance(5 * time.Minute)
	for range 3 {
		guard.Failed("anna", "10.0.0.1")
	}
	if left := guard.Blocked("anna", "10.0.0.9"); left != 15*time.Minute {
		t.Errorf("left = %v, want the name's longer block", left)
	}
}

// New limits apply to the next failure; a block already running keeps the
// length it started with.
func TestNewLimitsApplyToTheNextFailure(t *testing.T) {
	guard, _ := newGuard(t, school)
	for range 3 {
		guard.Failed("anna", "10.0.0.1")
	}
	guard.SetLimits(lockout.Limits{AccountFailures: 1, AddressFailures: 5, Block: time.Minute})
	if got := guard.Limits(); got.AccountFailures != 1 || got.Block != time.Minute {
		t.Errorf("limits = %+v", got)
	}
	if left := guard.Blocked("anna", "10.0.0.1"); left != 15*time.Minute {
		t.Errorf("the running block = %v, want the 15 minutes it started with", left)
	}
	guard.Failed("piotr", "10.0.0.2")
	if left := guard.Blocked("piotr", "10.0.0.3"); left != time.Minute {
		t.Errorf("one failure under a limit of 1 = %v, want a 1m block", left)
	}
}

// Without a name (a body the proxy could not read) only the address counts.
func TestAnEmptyNameCountsTheAddressOnly(t *testing.T) {
	guard, _ := newGuard(t, lockout.Limits{AccountFailures: 1, AddressFailures: 2, Block: time.Minute})
	if block := guard.Failed("", "10.0.0.1"); block.Account {
		t.Error("an empty name was blocked")
	}
	if left := guard.Blocked("", "10.0.0.2"); left != 0 {
		t.Errorf("an empty name from another address = %v", left)
	}
	guard.Failed("", "10.0.0.1")
	if guard.Blocked("anna", "10.0.0.1") == 0 {
		t.Error("the address was not counted")
	}
}

// Many names from many addresses cannot grow the tables without end.
func TestTheTablesStayBounded(t *testing.T) {
	guard, at := newGuard(t, lockout.Limits{AccountFailures: 100, AddressFailures: 1000, Block: time.Minute})
	for index := range lockout.MaxTracked + 500 {
		guard.Failed(fmt.Sprintf("name-%d", index), fmt.Sprintf("10.%d.%d.%d", index>>16&255, index>>8&255, index&255))
		if index%1000 == 0 {
			at.advance(time.Second)
		}
	}
	if names, addresses := guard.Tracked(); names > lockout.MaxTracked || addresses > lockout.MaxTracked {
		t.Errorf("tracked %d names and %d addresses, want at most %d each", names, addresses, lockout.MaxTracked)
	}
}

func TestLimitsOutsideTheirRangeAreNamed(t *testing.T) {
	for _, test := range []struct {
		limits lockout.Limits
		field  string
	}{
		{lockout.Limits{AccountFailures: 0, AddressFailures: 5, Block: time.Minute}, "account_failures"},
		{lockout.Limits{AccountFailures: 101, AddressFailures: 5, Block: time.Minute}, "account_failures"},
		{lockout.Limits{AccountFailures: 5, AddressFailures: 1001, Block: time.Minute}, "address_failures"},
		{lockout.Limits{AccountFailures: 5, AddressFailures: 5, Block: 0}, "block_minutes"},
		{lockout.Limits{AccountFailures: 5, AddressFailures: 5, Block: 1441 * time.Minute}, "block_minutes"},
	} {
		if got := test.limits.Invalid(); got != test.field {
			t.Errorf("%+v: invalid field = %q, want %q", test.limits, got, test.field)
		}
	}
	if got := school.Invalid(); got != "" {
		t.Errorf("valid limits named %q", got)
	}
}
