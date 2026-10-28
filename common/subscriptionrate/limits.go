// Package subscriptionrate provides per-subscription, per-core rate budgets.
package subscriptionrate

import "golang.org/x/time/rate"

// Burst is the maximum initial credit in bytes, independent of the configured rate.
const Burst = 8192

// MaxRate bounds configuration to a practical, exactly representable byte rate.
const MaxRate = uint64(1 << 40)

// Limits must be shared by all connections and identities of a subscription.
// A nil limiter means that direction is unlimited. Limits are immutable after creation.
type Limits struct {
	Uplink   *rate.Limiter
	Downlink *rate.Limiter
}

func New(uplink, downlink uint64) *Limits {
	return &Limits{Uplink: newLimiter(uplink), Downlink: newLimiter(downlink)}
}

func newLimiter(bytesPerSecond uint64) *rate.Limiter {
	if bytesPerSecond == 0 {
		return nil
	}
	return rate.NewLimiter(rate.Limit(bytesPerSecond), Burst)
}
