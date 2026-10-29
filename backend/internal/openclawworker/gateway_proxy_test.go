package openclawworker

import (
	"strings"
	"testing"
)

func TestGatewayCellRejectsUnsafeRuntimeMapping(t *testing.T) {
	tenant := "u1-0123456789ab"
	valid := `{"id":"` + strings.Repeat("a", 64) + `","name":"openclaw-cell-` + tenant + `","running":true,"ports":{"18789/tcp":[{"HostIp":"127.0.0.1","HostPort":"19100"}]}}`
	for _, test := range []struct{ old, replacement string }{
		{`127.0.0.1`, `0.0.0.0`}, {`19100`, `19201`}, {`19100`, `8080`},
		{`"running":true`, `"running":false`}, {tenant, "u2-abcdef012345"}, {`18789/tcp`, `19201/tcp`},
	} {
		if _, err := parseGatewayCell([]byte(strings.ReplaceAll(valid, test.old, test.replacement)), tenant); err == nil {
			t.Fatalf("accepted unsafe mapping %s", test.replacement)
		}
	}
	target, err := parseGatewayCell([]byte(valid), tenant)
	if err != nil || target.Address != "127.0.0.1:19100" {
		t.Fatalf("valid mapping: %v, %v", target, err)
	}
}
