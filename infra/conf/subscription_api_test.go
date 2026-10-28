package conf_test

import (
	"testing"

	"github.com/xtls/xray-core/infra/conf"
)

func TestSubscriptionAPIRequiresLocalListen(t *testing.T) {
	for _, listen := range []string{"", ":10085", "0.0.0.0:10085", "192.168.1.2:10085", "localhost:10085"} {
		if _, err := (&conf.APIConfig{Tag: "api", Listen: listen, Services: []string{"SubscriptionService"}}).Build(); err == nil {
			t.Fatal("unsafe listener accepted", listen)
		}
	}
	for _, listen := range []string{"127.0.0.1:10085", "[::1]:10085", "/run/xray/private.sock"} {
		if _, err := (&conf.APIConfig{Tag: "api", Listen: listen, Services: []string{"SubscriptionService"}}).Build(); err != nil {
			t.Fatal(err)
		}
	}
}
