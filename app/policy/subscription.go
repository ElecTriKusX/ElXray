package policy

import (
	"fmt"
	"strings"

	"github.com/xtls/xray-core/common/subscriptionrate"
)

// BuildSubscriptionRates validates configuration and creates one budget per subscription.
// Email is Xray's authenticated accounting identity, not a value supplied in a request header.
func BuildSubscriptionRates(config map[string]*SubscriptionPolicy) (map[string]*subscriptionrate.Limits, error) {
	byEmail := make(map[string]*subscriptionrate.Limits)
	owners := make(map[string]string)
	for id, p := range config {
		if strings.TrimSpace(id) == "" || p == nil || len(p.Emails) == 0 {
			return nil, fmt.Errorf("subscription %q requires an id, policy and at least one email", id)
		}
		if p.UplinkBps > subscriptionrate.MaxRate || p.DownlinkBps > subscriptionrate.MaxRate {
			return nil, fmt.Errorf("subscription %q rate exceeds %d bytes/s", id, subscriptionrate.MaxRate)
		}
		limits := subscriptionrate.New(p.UplinkBps, p.DownlinkBps)
		for _, email := range p.Emails {
			if email == "" || strings.TrimSpace(email) != email {
				return nil, fmt.Errorf("subscription %q has an empty or untrimmed email", id)
			}
			// VLESS/Trojan validators use case-insensitive email identities.
			email = strings.ToLower(email)
			if owner, exists := owners[email]; exists {
				return nil, fmt.Errorf("email %q is repeated in subscriptions %q and %q", email, owner, id)
			}
			owners[email] = id
			if p.UplinkBps != 0 || p.DownlinkBps != 0 {
				byEmail[email] = limits
			}
		}
	}
	return byEmail, nil
}
