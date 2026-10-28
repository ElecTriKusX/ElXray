package policy

import (
	"context"
	"testing"

	"github.com/xtls/xray-core/common/subscriptionrate"
)

func TestSubscriptionBudgets(t *testing.T) {
	config := &Config{Subscriptions: map[string]*SubscriptionPolicy{
		"account": {Emails: []string{"phone", "desktop"}, UplinkBps: 1000, DownlinkBps: 2000},
		"other":   {Emails: []string{"other"}, UplinkBps: 1000},
		"free":    {Emails: []string{"free"}},
	}}
	m, err := New(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	a, b := m.ForEmail("phone"), m.ForEmail("desktop")
	if a == nil || a != b {
		t.Fatal("subscription identities must share a budget")
	}
	if a.Uplink == a.Downlink {
		t.Fatal("upload and download must have independent budgets")
	}
	if a == m.ForEmail("other") {
		t.Fatal("different subscriptions must not share budgets")
	}
	if m.ForEmail("other").Downlink != nil {
		t.Fatal("zero means unlimited")
	}
	if m.ForEmail("missing") != nil || m.ForEmail("free") != nil {
		t.Fatal("unrestricted identities must have no wrapper")
	}
	node, err := New(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if node.ForEmail("phone").Uplink == a.Uplink {
		t.Fatal("budgets must be scoped to the core instance")
	}
}

func TestInvalidSubscriptionPolicies(t *testing.T) {
	cases := []map[string]*SubscriptionPolicy{
		{"": {Emails: []string{"a"}}},
		{"a": nil},
		{"a": {}},
		{"a": {Emails: []string{""}}},
		{"a": {Emails: []string{" a"}}},
		{"a": {Emails: []string{"a", "a"}}},
		{"a": {Emails: []string{"a"}}, "b": {Emails: []string{"a"}}},
		{"a": {Emails: []string{"a"}, UplinkBps: subscriptionrate.MaxRate + 1}},
	}
	for i, config := range cases {
		if _, err := New(context.Background(), &Config{Subscriptions: config}); err == nil {
			t.Errorf("case %d accepted invalid policy", i)
		}
	}
}
