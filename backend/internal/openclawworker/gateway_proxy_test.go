package openclawworker

import (
	"strings"
	"testing"
)

func TestGatewayCellRejectsUnsafeRuntimeMapping(t *testing.T) {
	tenant := statusTenant
	valid := string(statusCellJSON("running", true))
	for _, test := range []struct{ old, replacement string }{
		{`127.0.0.1`, `0.0.0.0`}, {`127.0.0.1`, `::1`}, {`19100`, `19200`}, {`19100`, `19099`}, {`19100`, `8080`}, {`19100`, `bad`},
		{`"running":true`, `"running":false`}, {tenant, "u2-abcdef012345"}, {`18789/tcp`, `19201/tcp`},
		{strings.Repeat("a", 64), strings.Repeat("a", 63)}, {strings.Repeat("a", 64), strings.Repeat("z", 64)},
		{`"status":"running"`, `"status":"exited"`}, {`"status":"running"`, `"status":"paused"`},
		{`"paused":false`, `"paused":true`}, {`"restarting":false`, `"restarting":true`}, {`"dead":false`, `"dead":true`},
		{`"running":true,`, ``}, {`"status":"running",`, ``}, {`"paused":false,`, ``}, {`"dead":false`, `"dead":null`},
		{`[{"HostIp":"127.0.0.1","HostPort":"19100"}]`, `[]`},
		{`[{"HostIp":"127.0.0.1","HostPort":"19100"}]`, `[{"HostIp":"127.0.0.1","HostPort":"19100"},{"HostIp":"127.0.0.1","HostPort":"19101"}]`},
	} {
		out := []byte(strings.ReplaceAll(valid, test.old, test.replacement))
		if _, err := parseGatewayCell(out, tenant); err == nil {
			t.Fatalf("accepted unsafe mapping %s", test.replacement)
		}
		if _, err := parseCellObservation(out, tenant); err == nil {
			t.Fatalf("status accepted unsafe mapping %s", test.replacement)
		}
	}
	for _, out := range [][]byte{[]byte(`[]`), []byte(`{`), append([]byte(valid), []byte(valid)...), []byte(strings.Repeat(" ", 16<<10) + valid)} {
		if _, err := parseGatewayCell(out, tenant); err == nil {
			t.Fatal("accepted malformed/ambiguous/oversized inspection")
		}
	}
	for _, port := range []string{"19100", "19199"} {
		for _, prefix := range []string{"", "/"} {
			out := strings.ReplaceAll(valid, "19100", port)
			out = strings.Replace(out, `"name":"openclaw-cell-`, `"name":"`+prefix+`openclaw-cell-`, 1)
			target, err := parseGatewayCell([]byte(out), tenant)
			if err != nil || target.ID != strings.Repeat("a", 64) || target.Address != "127.0.0.1:"+port {
				t.Fatalf("valid mapping: %v, %v", target, err)
			}
		}
	}
	if _, err := parseGatewayCell([]byte(valid), "invalid;tenant"); err == nil {
		t.Fatal("accepted invalid tenant")
	}
}

func TestGatewayCellStillRejectsStoppedRuntime(t *testing.T) {
	for _, state := range []string{"stopped", "exited"} {
		out := statusCellJSON(state, false)
		if _, err := parseGatewayCell(out, statusTenant); err == nil {
			t.Fatalf("gateway proxy accepted %s container", state)
		}
		if observed, err := parseCellObservation(out, statusTenant); err != nil || observed.State != "stopped" {
			t.Fatalf("status rejected explicit %s state: %+v %v", state, observed, err)
		}
		for _, invalid := range []struct{ old, replacement string }{
			{statusTenant, "u2-abcdef012345"}, {strings.Repeat("a", 64), "short-id"},
			{`"running":false,`, ``}, {`"status":"` + state + `"`, `"status":"created"`},
			{`"paused":false`, `"paused":true`}, {`127.0.0.1`, `0.0.0.0`}, {`19100`, `19200`}, {`18789/tcp`, `19201/tcp`},
		} {
			if _, err := parseCellObservation([]byte(strings.ReplaceAll(string(out), invalid.old, invalid.replacement)), statusTenant); err == nil {
				t.Fatalf("stopped state bypassed identity/runtime validation: %s", invalid.replacement)
			}
		}
	}
}
