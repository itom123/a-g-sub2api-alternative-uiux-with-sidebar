package openclawgateway

import "encoding/hex"

// EnrollmentCommand crosses only the authenticated controller/worker boundary.
// Registration carries a digest; QR tickets and native credentials never do.
type EnrollmentCommand struct {
	Action          string                      `json:"action"`
	ID              string                      `json:"id,omitempty"`
	Client          string                      `json:"client,omitempty"`
	TicketDigest    string                      `json:"ticket_digest,omitempty"`
	CreatedAtMS     int64                       `json:"created_at_ms,omitempty"`
	ExpiresAtMS     int64                       `json:"expires_at_ms,omitempty"`
	Binding         *EnrollmentBindingTransport `json:"binding,omitempty"`
	ArchivedReceipt *EnrollmentReceipt          `json:"archived_receipt,omitempty"`
}

type EnrollmentReceipt struct {
	ID                string                      `json:"id"`
	Client            string                      `json:"client"`
	State             EnrollmentState             `json:"state"`
	CreatedAtMS       int64                       `json:"created_at_ms"`
	ExpiresAtMS       int64                       `json:"expires_at_ms"`
	DeliveredAtMS     int64                       `json:"delivered_at_ms,omitempty"`
	DeliveryUncertain bool                        `json:"delivery_uncertain,omitempty"`
	RetirementReady   bool                        `json:"retirement_ready,omitempty"`
	Binding           *EnrollmentBindingTransport `json:"binding,omitempty"`
}

type EnrollmentBridgeResult struct {
	Protocol         string              `json:"protocol"`
	SessionDelivery  bool                `json:"session_delivery"`
	Enrollment       *EnrollmentReceipt  `json:"enrollment,omitempty"`
	Enrollments      []EnrollmentReceipt `json:"enrollments,omitempty"`
	CleanupRequestID string              `json:"cleanup_request_id,omitempty"`
	RetiredID        string              `json:"retired_id,omitempty"`
}

func (c EnrollmentCommand) Validate() error {
	if c.Action == "list" && c.ID == "" {
		return nil
	}
	if !enrollmentRequestID(c.ID) {
		return ErrEnrollmentTransition
	}
	switch c.Action {
	case "retire":
		if c.ArchivedReceipt == nil || c.ArchivedReceipt.ID != c.ID || !c.ArchivedReceipt.Retirable() {
			return ErrEnrollmentTransition
		}
	case "register":
		if c.Client != "ios" && c.Client != "macos" {
			return ErrEnrollmentTransition
		}
		if len(c.TicketDigest) != 64 {
			return ErrEnrollmentTransition
		}
		for _, v := range c.TicketDigest {
			if !(v >= '0' && v <= '9' || v >= 'a' && v <= 'f') {
				return ErrEnrollmentTransition
			}
		}
		if c.CreatedAtMS <= 0 || c.ExpiresAtMS <= c.CreatedAtMS || c.ExpiresAtMS-c.CreatedAtMS != EnrollmentTTL.Milliseconds() {
			return ErrEnrollmentTransition
		}
	case "approve":
		if c.Binding == nil || c.Binding.EnrollmentID != c.ID {
			return ErrEnrollmentTransition
		}
		if !validEnrollmentBinding(c.Binding.Binding(), "ios") {
			return ErrEnrollmentTransition
		}
	case "status", "reject", "cancel", "seal":
	default:
		return ErrEnrollmentTransition
	}
	return nil
}

func (r EnrollmentReceipt) Retirable() bool {
	if r.Validate() != nil || r.DeliveryUncertain || !r.RetirementReady {
		return false
	}
	switch r.State {
	case EnrollmentExpired, EnrollmentRejected, EnrollmentCancelled, EnrollmentConnected:
		return true
	}
	return false
}

func (r EnrollmentReceipt) Validate() error {
	if !enrollmentRequestID(r.ID) || (r.Client != "ios" && r.Client != "macos") || r.CreatedAtMS <= 0 || r.ExpiresAtMS <= r.CreatedAtMS || r.ExpiresAtMS-r.CreatedAtMS != EnrollmentTTL.Milliseconds() {
		return ErrEnrollmentTransition
	}
	switch r.State {
	case EnrollmentCreated, EnrollmentPending, EnrollmentApproved, EnrollmentConnected, EnrollmentExpired, EnrollmentRejected, EnrollmentCancelled:
	default:
		return ErrEnrollmentTransition
	}
	if r.Binding != nil && (r.Binding.EnrollmentID != r.ID || !validEnrollmentBinding(r.Binding.Binding(), r.Client)) {
		return ErrEnrollmentTransition
	}
	if (r.State == EnrollmentPending || r.State == EnrollmentApproved || r.State == EnrollmentConnected || r.State == EnrollmentRejected) && r.Binding == nil {
		return ErrEnrollmentTransition
	}
	if r.State == EnrollmentCreated && r.Binding != nil {
		return ErrEnrollmentTransition
	}
	if r.State == EnrollmentConnected {
		if r.DeliveredAtMS < r.CreatedAtMS || r.DeliveredAtMS >= r.ExpiresAtMS {
			return ErrEnrollmentTransition
		}
	} else if r.DeliveredAtMS != 0 {
		return ErrEnrollmentTransition
	}
	if r.DeliveryUncertain && r.State != EnrollmentApproved {
		return ErrEnrollmentTransition
	}
	if r.RetirementReady && (r.State == EnrollmentCreated || r.State == EnrollmentPending || r.State == EnrollmentApproved) {
		return ErrEnrollmentTransition
	}
	return nil
}

func (r EnrollmentReceipt) Matches(e Enrollment) bool {
	return r.Validate() == nil && r.ID == e.ID && r.Client == e.Client && r.CreatedAtMS == e.CreatedAt.UnixMilli() && r.ExpiresAtMS == e.ExpiresAt.UnixMilli()
}

func EnrollmentRegistration(e Enrollment, ticket string) (EnrollmentCommand, error) {
	digest, err := EnrollmentTicketDigest(ticket)
	if err != nil {
		return EnrollmentCommand{}, err
	}
	return EnrollmentCommand{Action: "register", ID: e.ID, Client: e.Client, TicketDigest: hex.EncodeToString(digest[:]), CreatedAtMS: e.CreatedAt.UnixMilli(), ExpiresAtMS: e.ExpiresAt.UnixMilli()}, nil
}
