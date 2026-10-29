package openclawgateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEnrollmentRetirementRequiresNativeSealAndTerminalReceipt(t *testing.T) {
	e, _, _ := enrollmentFixture(t)
	r := EnrollmentReceipt{ID: e.ID, Client: e.Client, State: EnrollmentExpired, CreatedAtMS: e.CreatedAt.UnixMilli(), ExpiresAtMS: e.ExpiresAt.UnixMilli()}
	c := EnrollmentCommand{Action: "retire", ID: e.ID, ArchivedReceipt: &r}
	if c.Validate() == nil {
		t.Fatal("unsealed native receipt authorized compaction")
	}
	r.RetirementReady = true
	if c.Validate() != nil {
		t.Fatal("sealed terminal receipt rejected")
	}
	r.State = EnrollmentCreated
	if c.Validate() == nil {
		t.Fatal("live admission authorized compaction")
	}
	r.State = EnrollmentExpired
	c.ID = "another-request"
	if c.Validate() == nil {
		t.Fatal("replacement request authorized compaction")
	}
}

func TestEnrollmentRegistrationCarriesDigestOnlyAndExactLifetime(t *testing.T) {
	e, _, _ := enrollmentFixture(t)
	ticket, err := NewEnrollmentTicket()
	if err != nil {
		t.Fatal(err)
	}
	c, err := EnrollmentRegistration(e, ticket)
	if err != nil || c.Validate() != nil {
		t.Fatal("registration rejected", err)
	}
	raw, _ := json.Marshal(c)
	if strings.Contains(string(raw), ticket) || len(c.TicketDigest) != 64 || c.ExpiresAtMS-c.CreatedAtMS != EnrollmentTTL.Milliseconds() {
		t.Fatal("invalid registration boundary")
	}
	for _, change := range []func(*EnrollmentCommand){
		func(c *EnrollmentCommand) { c.TicketDigest = strings.Repeat("g", 64) },
		func(c *EnrollmentCommand) { c.ExpiresAtMS++ },
		func(c *EnrollmentCommand) { c.Action = "shell" },
		func(c *EnrollmentCommand) { c.ID = "../bad" },
	} {
		invalid := c
		change(&invalid)
		if invalid.Validate() == nil {
			t.Fatal("invalid registration accepted")
		}
	}
}

func TestEnrollmentReceiptRequiresExactIdentityKeyAndPermissions(t *testing.T) {
	e, b, _ := enrollmentFixture(t)
	b.EnrollmentID = e.ID
	bt := b.Transport()
	r := EnrollmentReceipt{ID: e.ID, Client: e.Client, State: EnrollmentPending, CreatedAtMS: e.CreatedAt.UnixMilli(), ExpiresAtMS: e.ExpiresAt.UnixMilli(), Binding: &bt}
	if !r.Matches(e) {
		t.Fatal("valid receipt rejected")
	}
	for _, change := range []func(*EnrollmentReceipt){
		func(r *EnrollmentReceipt) { r.ID = "replacement-fixture" },
		func(r *EnrollmentReceipt) { r.CreatedAtMS++ },
		func(r *EnrollmentReceipt) { r.Binding = nil },
		func(r *EnrollmentReceipt) { b := *r.Binding; b.PublicKey = "bad-key"; r.Binding = &b },
		func(r *EnrollmentReceipt) { b := *r.Binding; b.Scopes = []string{"operator.admin"}; r.Binding = &b },
		func(r *EnrollmentReceipt) { b := *r.Binding; b.EnrollmentID = "another-enrollment"; r.Binding = &b },
	} {
		invalid := r
		change(&invalid)
		if invalid.Matches(e) {
			t.Fatal("unsafe receipt accepted")
		}
	}
}

func TestEnrollmentConnectedReceiptRequiresInWindowDeliveryTimestamp(t *testing.T) {
	e, b, _ := enrollmentFixture(t)
	b.EnrollmentID = e.ID
	bt := b.Transport()
	r := EnrollmentReceipt{ID: e.ID, Client: e.Client, State: EnrollmentConnected, CreatedAtMS: e.CreatedAt.UnixMilli(), ExpiresAtMS: e.ExpiresAt.UnixMilli(), Binding: &bt}
	if r.Validate() == nil {
		t.Fatal("connected receipt without delivery proof accepted")
	}
	r.DeliveredAtMS = r.ExpiresAtMS
	if r.Validate() == nil {
		t.Fatal("expired delivery accepted")
	}
	r.DeliveredAtMS = r.ExpiresAtMS - 1
	if r.Validate() != nil {
		t.Fatal("in-window native delivery rejected")
	}
	r.State = EnrollmentApproved
	if r.Validate() == nil {
		t.Fatal("non-delivered state carried delivery timestamp")
	}
}

func TestEnrollmentUncertainDeliveryIsNotConnectionProof(t *testing.T) {
	e, b, _ := enrollmentFixture(t)
	b.EnrollmentID = e.ID
	bt := b.Transport()
	r := EnrollmentReceipt{ID: e.ID, Client: e.Client, State: EnrollmentApproved, CreatedAtMS: e.CreatedAt.UnixMilli(), ExpiresAtMS: e.ExpiresAt.UnixMilli(), Binding: &bt, DeliveryUncertain: true}
	if !r.Matches(e) {
		t.Fatal("uncertain approved delivery was hidden")
	}
	for _, state := range []EnrollmentState{EnrollmentCreated, EnrollmentPending, EnrollmentConnected, EnrollmentExpired, EnrollmentRejected, EnrollmentCancelled} {
		r.State = state
		if state == EnrollmentConnected {
			r.DeliveredAtMS = r.CreatedAtMS + 1
		} else {
			r.DeliveredAtMS = 0
		}
		if r.Validate() == nil {
			t.Fatal("uncertain delivery accepted in unrelated state", state)
		}
	}
}
