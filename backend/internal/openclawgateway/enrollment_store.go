package openclawgateway

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

var ErrEnrollmentRateLimit = errors.New("too many device enrollment requests; try again later")

// EnrollmentStore is control-plane persistence. Every public operation must also
// pass the service's live workspace entitlement check. Native credential grants
// must consult this authority; this store is not a substitute for that gate.
// TicketSecret is shared only between the control plane and the private worker.
// It is never serialized into an enrollment response.
type EnrollmentStore struct {
	DB           *sql.DB
	TicketSecret []byte
}

// Private persistence includes the reviewed public key. Public API serialization
// of EnrollmentBinding intentionally excludes it.
type storedEnrollmentBinding struct {
	RequestID   string   `json:"request_id"`
	DeviceID    string   `json:"device_id"`
	DisplayName string   `json:"display_name,omitempty"`
	Platform    string   `json:"platform,omitempty"`
	PublicKey   string   `json:"public_key"`
	Roles       []string `json:"roles"`
	Scopes      []string `json:"scopes"`
}

func encodeEnrollmentBinding(b *EnrollmentBinding) ([]byte, error) {
	if b == nil {
		return nil, nil
	}
	return json.Marshal(storedEnrollmentBinding{RequestID: b.RequestID, DeviceID: b.DeviceID, DisplayName: b.DisplayName, Platform: b.Platform, PublicKey: b.PublicKey, Roles: b.Roles, Scopes: b.Scopes})
}

// Create returns the admission ticket once. Only its digest is stored. Owners
// are serialized for the quota check so concurrent creates cannot evade it.
func (s EnrollmentStore) Create(ctx context.Context, ownerID, instanceID int64, tenant, client string, now time.Time) (Enrollment, string, error) {
	var entropy [24]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return Enrollment{}, "", err
	}
	e, err := NewEnrollment(base64.RawURLEncoding.EncodeToString(entropy[:]), ownerID, instanceID, tenant, client, now)
	if err != nil {
		return Enrollment{}, "", err
	}
	var ticket string
	if len(s.TicketSecret) >= 32 {
		ticket, err = NewEnrollmentAdmissionTicket(tenant, e.ID, e.ExpiresAt, s.TicketSecret)
	} else {
		ticket, err = NewEnrollmentTicket()
	}
	if err != nil {
		return Enrollment{}, "", err
	}
	digest, err := EnrollmentTicketDigest(ticket)
	if err != nil {
		return Enrollment{}, "", err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Enrollment{}, "", err
	}
	defer tx.Rollback()
	// Lock the owner row rather than an app-global lock; unrelated owners do not
	// block each other. This also serializes creation against account deletion.
	var lockedOwner int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id = $1 AND deleted_at IS NULL AND status = 'active' FOR UPDATE`, ownerID).Scan(&lockedOwner); err != nil {
		return Enrollment{}, "", ErrEnrollmentTransition
	}
	var recent, live int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE state IN ('created', 'awaiting_approval', 'approved') AND expires_at > $2) FROM openclaw_device_enrollments WHERE owner_id = $1 AND created_at > $2 - INTERVAL '1 hour'`, ownerID, now).Scan(&recent, &live); err != nil {
		return Enrollment{}, "", err
	}
	if recent >= 10 || live >= 5 {
		return Enrollment{}, "", ErrEnrollmentRateLimit
	}
	// The service rechecks linked billing entitlement too. This SQL additionally
	// prevents accidentally persisting a session for somebody else's instance.
	result, err := tx.ExecContext(ctx, `INSERT INTO openclaw_device_enrollments (id, owner_id, instance_id, tenant_slug, client, state, ticket_digest, created_at, expires_at, updated_at) SELECT $1, $2, i.id, i.tenant_slug, $5, 'created', $6, $7, $8, $7 FROM openclaw_instances i WHERE i.id = $3 AND i.user_id = $2 AND i.tenant_slug = $4 AND i.status IN ('ready', 'running') AND i.deletion_scheduled_at IS NULL`, e.ID, ownerID, instanceID, tenant, client, digest[:], e.CreatedAt, e.ExpiresAt)
	if err != nil {
		return Enrollment{}, "", err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return Enrollment{}, "", ErrEnrollmentTransition
	}
	if err := recordEnrollmentEvent(ctx, tx, e, now); err != nil {
		return Enrollment{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return Enrollment{}, "", err
	}
	return e, ticket, nil
}

type EnrollmentMutation struct {
	Action      string
	Binding     EnrollmentBinding
	DeliveredAt time.Time
}

// ApprovalCandidates consults durable controller records even if a worker has
// restarted and lost its transient inventory annotations. A still-unbound live
// registration, including expired ones, must be reconciled before allowing a
// legacy approval fallback. Unavailable native state fails closed.
func (s EnrollmentStore) ApprovalCandidates(ctx context.Context, ownerID, instanceID int64, deviceID, requestID string) ([]string, error) {
	if s.DB == nil || ownerID <= 0 || instanceID <= 0 || !enrollmentRequestID(deviceID) || !enrollmentRequestID(requestID) {
		return nil, ErrEnrollmentTransition
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM openclaw_device_enrollments WHERE owner_id = $1 AND instance_id = $2 AND (binding->>'request_id' = $4 OR (native_archived_receipt IS NULL AND (binding->>'device_id' = $3 OR binding IS NULL))) ORDER BY created_at DESC LIMIT 101`, ownerID, instanceID, deviceID, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if !enrollmentRequestID(id) || len(ids) >= 100 {
			return nil, ErrEnrollmentTransition
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Mutate locks the exact owner/workspace/session row and writes the transition
// and its credential-free audit event together. Never perform network calls
// while holding this transaction. A native approval must still recheck the
// reviewed request at its mutation boundary, then provide a verified receipt.
func (s EnrollmentStore) Mutate(ctx context.Context, ownerID, instanceID int64, id string, mutation EnrollmentMutation, now time.Time) (Enrollment, error) {
	if !enrollmentRequestID(id) || ownerID <= 0 || instanceID <= 0 {
		return Enrollment{}, ErrEnrollmentTransition
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Enrollment{}, err
	}
	defer tx.Rollback()
	var e Enrollment
	var rawBinding []byte
	err = tx.QueryRowContext(ctx, `SELECT id, owner_id, instance_id, tenant_slug, client, state, created_at, expires_at, binding FROM openclaw_device_enrollments WHERE id = $1 AND owner_id = $2 AND instance_id = $3 FOR UPDATE`, id, ownerID, instanceID).Scan(&e.ID, &e.OwnerID, &e.InstanceID, &e.Tenant, &e.Client, &e.State, &e.CreatedAt, &e.ExpiresAt, &rawBinding)
	if errors.Is(err, sql.ErrNoRows) {
		return Enrollment{}, ErrEnrollmentTransition
	}
	if err != nil {
		return Enrollment{}, err
	}
	if len(rawBinding) > 0 {
		var b storedEnrollmentBinding
		if len(rawBinding) > 16<<10 || json.Unmarshal(rawBinding, &b) != nil {
			return Enrollment{}, ErrEnrollmentTransition
		}
		e.Binding = &EnrollmentBinding{RequestID: b.RequestID, DeviceID: b.DeviceID, DisplayName: b.DisplayName, Platform: b.Platform, PublicKey: b.PublicKey, Roles: b.Roles, Scopes: b.Scopes}
		if !validEnrollmentBinding(*e.Binding, e.Client) {
			return Enrollment{}, ErrEnrollmentTransition
		}
	}
	previous := e.State
	transitionTime := now
	if mutation.Action == "connected" && !mutation.DeliveredAt.IsZero() {
		if mutation.DeliveredAt.After(now) {
			return Enrollment{}, ErrEnrollmentTransition
		}
		transitionTime = mutation.DeliveredAt
	}
	if mutation.Action != "inspect" && e.Expire(transitionTime) {
		err = ErrEnrollmentTransition
	} else {
		switch mutation.Action {
		case "status", "inspect":
		case "bind":
			err = e.Bind(mutation.Binding, now)
		case "approve":
			err = e.Approve(ownerID, instanceID, mutation.Binding, now)
		case "connected":
			err = e.Connected(mutation.Binding, transitionTime)
		case "reject":
			err = e.Close(ownerID, instanceID, EnrollmentRejected, now)
		case "cancel":
			err = e.Close(ownerID, instanceID, EnrollmentCancelled, now)
		default:
			err = ErrEnrollmentTransition
		}
	}
	transitionErr := err
	if e.State != previous {
		binding, err := encodeEnrollmentBinding(e.Binding)
		if err != nil {
			return Enrollment{}, err
		}
		var storedBinding any
		if binding != nil {
			storedBinding = string(binding)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE openclaw_device_enrollments SET state = $2, binding = $3::jsonb, updated_at = $4 WHERE id = $1`, e.ID, e.State, storedBinding, now); err != nil {
			return Enrollment{}, err
		}
		if err := recordEnrollmentEvent(ctx, tx, e, now); err != nil {
			return Enrollment{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Enrollment{}, err
	}
	if mutation.Action == "status" && e.State == EnrollmentExpired {
		transitionErr = nil
	}
	return e, transitionErr
}

func recordEnrollmentEvent(ctx context.Context, tx *sql.Tx, e Enrollment, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO openclaw_device_enrollment_events (enrollment_id, owner_id, instance_id, state, occurred_at) VALUES ($1, $2, $3, $4, $5)`, e.ID, e.OwnerID, e.InstanceID, e.State, now)
	return err
}
