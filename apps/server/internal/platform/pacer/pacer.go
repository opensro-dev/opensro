/*
===========================================================================

pacer.go - keyed token buckets that bound what one caller can make us do

A Keyed pacer holds one token bucket per key (a client address, an
account): each call takes a token, a bucket refills one token per
Refill up to Burst, and an empty bucket refuses. Refusing costs one
map read under a mutex, so a caller looping as fast as it can costs
nothing past the allowance. The key count is bounded (MaxKeys): past
it the least recently seen bucket goes, which can only ever hand a
forgotten caller a fresh allowance, never refuse an honest one.

The Agent's login, incident and refusal-line pacing and the GameWorld
ticket mints share this one implementation.

===========================================================================
*/

package pacer

import (
	"sync"
	"time"
)

/*
================
Config

Burst is how many calls a key may make at once, Refill how long one
token takes to come back, MaxKeys how many keys are tracked. Now is
the clock (time.Now when nil); tests inject one.
================
*/
type Config struct {
	Burst   int
	Refill  time.Duration
	MaxKeys int
	Now     func() time.Time
}

/*
================
bucket
================
*/
type bucket struct {
	tokens     int
	lastRefill time.Time
	lastSeen   time.Time
}

/*
================
Keyed

One bucket per key. Safe for concurrent use.
================
*/
type Keyed struct {
	mu      sync.Mutex
	config  Config
	buckets map[string]bucket
}

/*
================
New

Panics on a non-positive Burst, Refill or MaxKeys: the values are
compiled-in constants, so a bad one is a programmer error.
================
*/
func New(config Config) *Keyed {
	if config.Burst <= 0 || config.Refill <= 0 || config.MaxKeys <= 0 {
		panic("pacer: Burst, Refill and MaxKeys must be positive")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Keyed{config: config, buckets: make(map[string]bucket)}
}

/*
================
Allow

Takes one token from key's bucket; false when it is empty.
================
*/
func (pacer *Keyed) Allow(key string) bool {
	allowed, _ := pacer.Take(key)
	return allowed
}

/*
================
Take

Takes one token from key's bucket. A refusal also says how long until
the bucket holds a token again, for a Retry-After answer.
================
*/
func (pacer *Keyed) Take(key string) (bool, time.Duration) {
	now := pacer.config.Now()

	pacer.mu.Lock()
	defer pacer.mu.Unlock()

	current, exists := pacer.buckets[key]
	if !exists {
		if len(pacer.buckets) >= pacer.config.MaxKeys {
			pacer.evictOldestLocked()
		}
		current = bucket{tokens: pacer.config.Burst, lastRefill: now}
	}
	if elapsed := now.Sub(current.lastRefill); elapsed >= pacer.config.Refill {
		refills := int(elapsed / pacer.config.Refill)
		current.tokens = min(pacer.config.Burst, current.tokens+refills)
		current.lastRefill = current.lastRefill.Add(time.Duration(refills) * pacer.config.Refill)
	}
	current.lastSeen = now
	if current.tokens == 0 {
		pacer.buckets[key] = current
		return false, current.lastRefill.Add(pacer.config.Refill).Sub(now)
	}
	current.tokens--
	pacer.buckets[key] = current
	return true, 0
}

/*
================
evictOldestLocked

(pacer.mu held.) Forgets the least recently seen bucket. A linear scan:
it runs only when a new key arrives at the bound.
================
*/
func (pacer *Keyed) evictOldestLocked() {
	var (
		oldestKey string
		oldest    time.Time
		found     bool
	)
	for key, current := range pacer.buckets {
		if !found || current.lastSeen.Before(oldest) {
			oldestKey = key
			oldest = current.lastSeen
			found = true
		}
	}
	if found {
		delete(pacer.buckets, oldestKey)
	}
}
