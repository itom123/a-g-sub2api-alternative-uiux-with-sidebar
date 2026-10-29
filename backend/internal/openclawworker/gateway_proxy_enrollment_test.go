package openclawworker

import (
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"
)

func TestWorkerAdmissionTicketIsTenantBoundAndOnlyStripsValidBearer(t *testing.T) {
	secret := []byte(strings.Repeat("w", 32))
	ticket, err := openclawgateway.NewEnrollmentAdmissionTicket("u42-123456789abc", "enrollment-12345678", time.Now().Add(time.Minute), secret)
	if err != nil {
		t.Fatal(err)
	}
	w := &Worker{EnrollmentSecret: secret}
	if got, _ := w.enrollmentAdmission("u42-123456789abc", "Bearer "+ticket); got != "enrollment-12345678" {
		t.Fatal("valid ticket was not admitted", got)
	}
	for _, request := range [][2]string{{"u43-123456789abc", "Bearer " + ticket}, {"u42-123456789abc", ticket}, {"u42-123456789abc", "Bearer " + ticket + "x"}, {"u42-123456789abc", "Bearer shared-token"}} {
		if got, _ := w.enrollmentAdmission(request[0], request[1]); got != "" {
			t.Fatal("invalid ticket bypassed admission", got)
		}
	}
}
