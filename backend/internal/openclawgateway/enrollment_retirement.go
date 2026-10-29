package openclawgateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"time"
)

const EnrollmentRetirementGrace = time.Hour

type EnrollmentRetirementCandidate struct {
	ID       string
	Archived *EnrollmentReceipt
}

// RetirementBatch never discards history or bindings. An archived receipt is a
// durable acknowledgement that native compaction may proceed, and is retried
// until the native side's exact acknowledgement has itself been committed.
func (s EnrollmentStore) RetirementBatch(ctx context.Context, ownerID, instanceID int64, now time.Time) ([]EnrollmentRetirementCandidate, error) {
	if s.DB == nil || ownerID <= 0 || instanceID <= 0 || now.IsZero() {
		return nil, ErrEnrollmentTransition
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, native_archived_receipt FROM openclaw_device_enrollments WHERE owner_id = $1 AND instance_id = $2 AND expires_at <= $3 AND native_retired_at IS NULL ORDER BY expires_at, id LIMIT 50`, ownerID, instanceID, now.Add(-EnrollmentRetirementGrace))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []EnrollmentRetirementCandidate
	for rows.Next() {
		var c EnrollmentRetirementCandidate
		var raw []byte
		if err := rows.Scan(&c.ID, &raw); err != nil {
			return nil, err
		}
		if !enrollmentRequestID(c.ID) {
			return nil, ErrEnrollmentTransition
		}
		if len(raw) > 0 {
			c.Archived = new(EnrollmentReceipt)
			if len(raw) > 16<<10 || json.Unmarshal(raw, c.Archived) != nil || c.Archived.ID != c.ID || !c.Archived.Retirable() || time.UnixMilli(c.Archived.ExpiresAtMS).Add(EnrollmentRetirementGrace).After(now) {
				return nil, ErrEnrollmentTransition
			}
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// ArchiveNativeReceipt commits the native terminal state, exact binding (or
// verified absence), and audit event BEFORE any private native retirement call.
// A crash before this commit retains native history. A crash after it cannot
// lose provenance even when the native acknowledgement is lost.
func (s EnrollmentStore) ArchiveNativeReceipt(ctx context.Context, ownerID, instanceID int64, receipt EnrollmentReceipt, now time.Time) (EnrollmentReceipt, error) {
	if s.DB == nil || ownerID <= 0 || instanceID <= 0 || !receipt.Retirable() || time.UnixMilli(receipt.ExpiresAtMS).Add(EnrollmentRetirementGrace).After(now) {
		return EnrollmentReceipt{}, ErrEnrollmentTransition
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return EnrollmentReceipt{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var e Enrollment
	var bindingRaw, archivedRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT id, owner_id, instance_id, tenant_slug, client, state, created_at, expires_at, binding, native_archived_receipt FROM openclaw_device_enrollments WHERE id = $1 AND owner_id = $2 AND instance_id = $3 FOR UPDATE`, receipt.ID, ownerID, instanceID).Scan(&e.ID, &e.OwnerID, &e.InstanceID, &e.Tenant, &e.Client, &e.State, &e.CreatedAt, &e.ExpiresAt, &bindingRaw, &archivedRaw)
	if err != nil {
		return EnrollmentReceipt{}, err
	}
	if !receipt.Matches(e) {
		return EnrollmentReceipt{}, ErrEnrollmentTransition
	}
	if len(archivedRaw) > 0 {
		var archived EnrollmentReceipt
		if len(archivedRaw) > 16<<10 || json.Unmarshal(archivedRaw, &archived) != nil || !reflect.DeepEqual(archived, receipt) {
			return EnrollmentReceipt{}, ErrEnrollmentTransition
		}
		if err := tx.Commit(); err != nil {
			return EnrollmentReceipt{}, err
		}
		return archived, nil
	}
	var nativeBinding *EnrollmentBinding
	if receipt.Binding != nil {
		b := receipt.Binding.Binding()
		nativeBinding = &b
	}
	if len(bindingRaw) > 0 {
		var old storedEnrollmentBinding
		if len(bindingRaw) > 16<<10 || json.Unmarshal(bindingRaw, &old) != nil || nativeBinding == nil {
			return EnrollmentReceipt{}, ErrEnrollmentTransition
		}
		e.Binding = &EnrollmentBinding{RequestID: old.RequestID, DeviceID: old.DeviceID, DisplayName: old.DisplayName, Platform: old.Platform, PublicKey: old.PublicKey, Roles: old.Roles, Scopes: old.Scopes}
		if !validEnrollmentBinding(*e.Binding, e.Client) || !sameEnrollmentBinding(*e.Binding, *nativeBinding) {
			return EnrollmentReceipt{}, ErrEnrollmentTransition
		}
	}
	if receipt.State == EnrollmentConnected && (e.State != EnrollmentApproved && e.State != EnrollmentConnected || e.Binding == nil) {
		return EnrollmentReceipt{}, ErrEnrollmentTransition
	}
	if e.State == EnrollmentConnected && receipt.State != EnrollmentConnected {
		return EnrollmentReceipt{}, ErrEnrollmentTransition
	}
	if (e.State == EnrollmentCancelled || e.State == EnrollmentRejected) && e.State != receipt.State {
		return EnrollmentReceipt{}, ErrEnrollmentTransition
	}
	encodedBinding, err := encodeEnrollmentBinding(nativeBinding)
	if err != nil {
		return EnrollmentReceipt{}, err
	}
	var binding any
	if encodedBinding != nil {
		binding = string(encodedBinding)
	}
	encodedReceipt, err := json.Marshal(receipt)
	if err != nil {
		return EnrollmentReceipt{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE openclaw_device_enrollments SET state = $2, binding = $3::jsonb, native_archived_receipt = $4::jsonb, updated_at = $5 WHERE id = $1`, e.ID, receipt.State, binding, string(encodedReceipt), now); err != nil {
		return EnrollmentReceipt{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO openclaw_device_enrollment_events (enrollment_id, owner_id, instance_id, state, occurred_at) VALUES ($1, $2, $3, $4, $5)`, e.ID, ownerID, instanceID, "native_archived", now); err != nil {
		return EnrollmentReceipt{}, err
	}
	if err = tx.Commit(); err != nil {
		return EnrollmentReceipt{}, err
	}
	return receipt, nil
}

func (s EnrollmentStore) ConfirmNativeRetirement(ctx context.Context, ownerID, instanceID int64, archived EnrollmentReceipt, now time.Time) error {
	if s.DB == nil || ownerID <= 0 || instanceID <= 0 || !archived.Retirable() || time.UnixMilli(archived.ExpiresAtMS).Add(EnrollmentRetirementGrace).After(now) {
		return ErrEnrollmentTransition
	}
	raw, err := json.Marshal(archived)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE openclaw_device_enrollments SET native_retired_at = $4 WHERE id = $1 AND owner_id = $2 AND instance_id = $3 AND native_archived_receipt = $5::jsonb AND native_retired_at IS NULL`, archived.ID, ownerID, instanceID, now, string(raw))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		var retired sql.NullTime
		if err = tx.QueryRowContext(ctx, `SELECT native_retired_at FROM openclaw_device_enrollments WHERE id = $1 AND owner_id = $2 AND instance_id = $3 AND native_archived_receipt = $4::jsonb`, archived.ID, ownerID, instanceID, string(raw)).Scan(&retired); err != nil || !retired.Valid {
			return ErrEnrollmentTransition
		}
	} else if count != 1 {
		return ErrEnrollmentTransition
	} else {
		if _, err = tx.ExecContext(ctx, `INSERT INTO openclaw_device_enrollment_events (enrollment_id, owner_id, instance_id, state, occurred_at) VALUES ($1, $2, $3, $4, $5)`, archived.ID, ownerID, instanceID, "native_retired", now); err != nil {
			return err
		}
	}
	return tx.Commit()
}
