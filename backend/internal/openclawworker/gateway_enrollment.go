package openclawworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"
)

type enrollmentCellCommand func(context.Context, string, []byte, ...string) ([]byte, error)

func gatewayEnrollmentOperation(ctx context.Context, tenant string, request openclawgateway.EnrollmentCommand, run enrollmentCellCommand) (*openclawgateway.EnrollmentBridgeResult, error) {
	if !tenantPattern.MatchString(tenant) || request.Validate() != nil {
		return nil, openclawgateway.ErrEnrollmentTransition
	}
	// Read and terminal operations remain available when enrollment is withdrawn.
	if (request.Action == "register" || request.Action == "approve") && os.Getenv("OPENCLAW_DEVICE_ENROLLMENT_ENABLED") != "1" {
		return nil, errors.New("device enrollment disabled")
	}
	if (request.Action == "seal" || request.Action == "retire") && os.Getenv("OPENCLAW_DEVICE_ENROLLMENT_RETENTION_ENABLED") != "1" {
		return nil, errors.New("enrollment retirement disabled")
	}
	input, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	out, err := run(ctx, tenant, input, "node", "/app/dist/ag-enrollment/authority-cli.mjs", tenant)
	if err != nil || len(out) > 256<<10 {
		return nil, errors.New("native enrollment unavailable")
	}
	var result openclawgateway.EnrollmentBridgeResult
	decoder := json.NewDecoder(bytes.NewReader(out))
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, openclawgateway.ErrEnrollmentTransition
	}
	if result.Protocol != "ag-enrollment-v1" {
		return nil, openclawgateway.ErrEnrollmentTransition
	}
	if request.Action == "retire" {
		if result.RetiredID != request.ID || result.Enrollment != nil || result.Enrollments != nil || result.CleanupRequestID != "" {
			return nil, openclawgateway.ErrEnrollmentTransition
		}
		return &result, nil
	}
	if result.RetiredID != "" {
		return nil, openclawgateway.ErrEnrollmentTransition
	}
	if request.Action == "list" {
		if result.Enrollment != nil || result.Enrollments == nil || len(result.Enrollments) > 1000 {
			return nil, openclawgateway.ErrEnrollmentTransition
		}
		for _, r := range result.Enrollments {
			if r.Validate() != nil {
				return nil, openclawgateway.ErrEnrollmentTransition
			}
		}
	} else if result.Enrollment == nil || result.Enrollment.Validate() != nil || result.Enrollment.ID != request.ID || result.Enrollments != nil {
		return nil, openclawgateway.ErrEnrollmentTransition
	}
	if result.CleanupRequestID != "" && ((request.Action != "cancel" && request.Action != "reject") || result.Enrollment == nil || result.Enrollment.Binding == nil || result.CleanupRequestID != result.Enrollment.Binding.RequestID) {
		return nil, openclawgateway.ErrEnrollmentTransition
	}
	return &result, nil
}
