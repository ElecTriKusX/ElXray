package conf_test

import (
	"encoding/json"
	"testing"

	"github.com/xtls/xray-core/common"
	. "github.com/xtls/xray-core/infra/conf"
)

func TestSubscriptionRateConfig(t *testing.T) {
	var config PolicyConfig
	if err := json.Unmarshal([]byte(`{"subscriptions":{"account":{"emails":["user"],"uplinkBps":1250000,"downlinkBps":6250000,"revision":7,"disabled":true}},"requireSubscription":true}`), &config); err != nil {
		t.Fatal(err)
	}
	built, err := config.Build()
	if err != nil {
		t.Fatal(err)
	}
	p := built.Subscriptions["account"]
	if !built.RequireSubscription || p == nil || p.Revision != 7 || !p.Disabled || p.UplinkBps != 1250000 || p.DownlinkBps != 6250000 || len(p.Emails) != 1 || p.Emails[0] != "user" {
		t.Fatal("JSON rates or subscription identity lost")
	}
	config.Subscriptions["duplicate"] = &SubscriptionPolicy{Emails: []string{"user"}}
	if _, err := config.Build(); err == nil {
		t.Fatal("duplicate subscription identity accepted")
	}
}

func TestSubscriptionRateRejectsInvalidJSONPolicies(t *testing.T) {
	for _, input := range []string{
		`{"subscriptions":{"a":{"emails":["u"],"uplinkBps":-1}}}`,
		`{"subscriptions":{"a":{"emails":["u"],"uplinkBps":0.5}}}`,
		`{"subscriptions":{"a":{"emails":["u"],"uplinkBps":18446744073709551616}}}`,
		`{"subscriptions":{"a":{"emails":["u"],"uplinkBps":1099511627777}}}`,
		`{"subscriptions":{"a":null}}`,
		`{"subscriptions":{"a":{"emails":[]}}}`,
		`{"subscriptions":{"a":{"emails":["u"]},"b":{"emails":["U"]}}}`,
	} {
		var config PolicyConfig
		if err := json.Unmarshal([]byte(input), &config); err != nil {
			continue
		}
		if _, err := config.Build(); err == nil {
			t.Errorf("accepted invalid policy: %s", input)
		}
	}
}

func TestBufferSize(t *testing.T) {
	cases := []struct {
		Input  int32
		Output int32
	}{
		{
			Input:  0,
			Output: 0,
		},
		{
			Input:  -1,
			Output: -1,
		},
		{
			Input:  1,
			Output: 1024,
		},
	}

	for _, c := range cases {
		bs := c.Input
		pConf := Policy{
			BufferSize: &bs,
		}
		p, err := pConf.Build()
		common.Must(err)
		if p.Buffer.Connection != c.Output {
			t.Error("expected buffer size ", c.Output, " but got ", p.Buffer.Connection)
		}
	}
}
