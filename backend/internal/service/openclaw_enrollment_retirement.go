package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"
)

func (s *OpenClawService) retireGatewayEnrollmentHistory(ctx context.Context, ownerID, instanceID int64, tenant string, bridge openClawEnrollmentClient) error {
	store := openclawgateway.EnrollmentStore{DB: s.db}
	candidates, err := store.RetirementBatch(ctx, ownerID, instanceID, time.Now().UTC())
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		archived := candidate.Archived
		if archived == nil {
			native, err := bridge.GatewayEnrollment(ctx, tenant, openclawgateway.EnrollmentCommand{Action: "seal", ID: candidate.ID})
			if err != nil || native == nil || native.Enrollment == nil || native.Enrollment.ID != candidate.ID {
				return ErrOpenClawProviderUnavailable
			}
			receipt, err := store.ArchiveNativeReceipt(ctx, ownerID, instanceID, *native.Enrollment, time.Now().UTC())
			if err != nil {
				return err
			}
			archived = &receipt
		}
		// Only the committed controller receipt can authorize compaction. Never
		// acknowledge native state directly from an uncommitted status response.
		native, err := bridge.GatewayEnrollment(ctx, tenant, openclawgateway.EnrollmentCommand{Action: "retire", ID: candidate.ID, ArchivedReceipt: archived})
		if err != nil || native == nil || native.RetiredID != candidate.ID {
			return ErrOpenClawProviderUnavailable
		}
		if err := store.ConfirmNativeRetirement(ctx, ownerID, instanceID, *archived, time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}
