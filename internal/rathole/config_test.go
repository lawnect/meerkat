package rathole

import (
	"encoding/base64"
	"github.com/lawnect/meerkat/internal/contract"
	"strings"
	"testing"
)

func TestPinnedTargetAndNoiseAuthentication(t *testing.T) {
	c := contract.Configuration{ConnectorID: "connector_1", ServiceID: "service_1", Revision: 1, Target: contract.Target{Host: "app", Port: 3000}, Tunnel: contract.Tunnel{RemoteAddress: "outlet:2333", ServerPublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), ServiceName: "service_1", ServiceToken: "abcdefghijklmnopqrstuvwxyz0123456789"}}
	b, err := ClientAt(c, "172.20.0.2:3000")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `local_addr = "172.20.0.2:3000"`) || !strings.Contains(string(b), `type = "noise"`) || !strings.Contains(string(b), "remote_public_key") {
		t.Fatal("local pin or server authentication missing")
	}
	for _, address := range []string{"8.8.8.8:3000", "169.254.169.254:3000", "172.20.0.2:5432", "evil.example.com:3000"} {
		if _, err := ClientAt(c, address); err == nil {
			t.Fatal("unsafe resolved target accepted")
		}
	}
	c.Tunnel.ServiceName = "x]\n[client]"
	if _, err := Client(c); err == nil {
		t.Fatal("TOML injection accepted")
	}
}
