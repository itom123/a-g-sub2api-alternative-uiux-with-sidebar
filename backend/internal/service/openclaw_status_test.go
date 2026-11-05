package service

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

type statusFleetStub struct {
	OpenClawFleetClient
	result OpenClawFleetResult
	err    error
	calls  int
}

func (f *statusFleetStub) Enabled() bool { return true }
func (f *statusFleetStub) Status(context.Context, string) (OpenClawFleetResult, error) {
	f.calls++
	return f.result, f.err
}

func statusInstanceRows(instance OpenClawInstance) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "tenant_slug", "name", "status", "worker_host", "port", "last_error", "created_at", "updated_at", "deletion_requested_at", "deletion_scheduled_at"}).AddRow(
		instance.ID, instance.TenantSlug, instance.Name, instance.Status, instance.WorkerHost, instance.Port, instance.LastError,
		instance.CreatedAt, instance.UpdatedAt, instance.DeletionRequestedAt, instance.DeletionScheduledAt)
}

func statusInstance() OpenClawInstance {
	return OpenClawInstance{ID: 7, TenantSlug: "u42-0123456789ab", Name: "Workspace", Status: "missing", WorkerHost: "default",
		CreatedAt: time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC)}
}

func expectStatusDashboardReads(mock sqlmock.Sqlmock, instance OpenClawInstance) {
	mock.ExpectQuery(`FROM openclaw_instances WHERE user_id = \$1 ORDER BY created_at DESC`).WithArgs(int64(42)).WillReturnRows(statusInstanceRows(instance))
	mock.ExpectQuery(`FROM openclaw_connections WHERE instance_id = \$1`).WithArgs(instance.ID).
		WillReturnRows(sqlmock.NewRows([]string{"total", "connected", "usage", "last_used"}).AddRow(2, 1, 4, nil))
}

func TestOpenClawDashboardReturnsRefreshedInstance(t *testing.T) {
	for _, health := range []bool{true, false} {
		t.Run(map[bool]string{true: "healthy", false: "health-unavailable"}[health], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			instance := statusInstance()
			expectStatusDashboardReads(mock, instance)
			port, latency := 19104, 8
			result := OpenClawFleetResult{State: "running", Port: port}
			if health {
				result.LatencyMS = &latency
			}
			updated := instance
			updated.Status, updated.Port, updated.UpdatedAt = "running", &port, instance.UpdatedAt.Add(time.Second)
			mock.ExpectQuery(`UPDATE openclaw_instances\s+SET status = \$1, port = COALESCE\(\$2::integer, port\), last_latency_ms = \$3::integer,\s+last_seen_at = CASE WHEN \$3::integer IS NOT NULL THEN NOW\(\) ELSE last_seen_at END,\s+updated_at = NOW\(\)\s+WHERE id = \$4 AND user_id = \$5 AND status = \$6 AND updated_at = \$7\s+AND deletion_requested_at IS NULL AND deletion_scheduled_at IS NULL\s+AND COALESCE\(last_error, ''\) = ''`).
				WithArgs("running", port, result.LatencyMS, instance.ID, int64(42), instance.Status, instance.UpdatedAt).
				WillReturnRows(statusInstanceRows(updated))
			fleet := &statusFleetStub{result: result}
			dashboard, err := NewOpenClawService(db, fleet).GetDashboard(context.Background(), 42)
			if err != nil {
				t.Fatal(err)
			}
			if dashboard.FleetState != "running" || dashboard.Instance.Status != "running" || dashboard.Instance.Port == nil || *dashboard.Instance.Port != port || !dashboard.Instance.UpdatedAt.Equal(updated.UpdatedAt) {
				t.Fatalf("stale dashboard: %+v", dashboard)
			}
			if health != (dashboard.LatencyMS != nil) {
				t.Fatalf("wrong health evidence: %+v", dashboard)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOpenClawDashboardExplicitOfflineObservation(t *testing.T) {
	for _, state := range []string{"stopped", "missing"} {
		t.Run(state, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			instance := statusInstance()
			instance.Status = "running"
			expectStatusDashboardReads(mock, instance)
			updated := instance
			updated.Status = state
			updated.UpdatedAt = instance.UpdatedAt.Add(time.Second)
			mock.ExpectQuery(`UPDATE openclaw_instances`).WithArgs(state, nil, nil, instance.ID, int64(42), instance.Status, instance.UpdatedAt).
				WillReturnRows(statusInstanceRows(updated))
			fleet := &statusFleetStub{result: OpenClawFleetResult{State: state}}
			dashboard, err := NewOpenClawService(db, fleet).GetDashboard(context.Background(), 42)
			if err != nil || dashboard.FleetState != state || dashboard.Instance.Status != state || dashboard.LatencyMS != nil {
				t.Fatalf("explicit %s observation = %+v, %v", state, dashboard, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOpenClawDashboardUnavailableNeverWritesTelemetry(t *testing.T) {
	for name, fleet := range map[string]*statusFleetStub{
		"busy":             {err: errors.New("openclaw worker returned HTTP 409")},
		"bridge-down":      {err: errors.New("connection refused")},
		"empty-state":      {result: OpenClawFleetResult{}},
		"unknown-state":    {result: OpenClawFleetResult{State: "unknown"}},
		"unsafe-port":      {result: OpenClawFleetResult{State: "running", Port: 80}},
		"negative-latency": {result: OpenClawFleetResult{State: "running", Port: 19100, LatencyMS: func() *int { n := -1; return &n }()}},
	} {
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			instance := statusInstance()
			instance.Status = "running"
			expectStatusDashboardReads(mock, instance)
			dashboard, err := NewOpenClawService(db, fleet).GetDashboard(context.Background(), 42)
			if err != nil {
				t.Fatal(err)
			}
			if dashboard.FleetState != "unavailable" || dashboard.Instance.Status != "running" || !dashboard.Instance.UpdatedAt.Equal(instance.UpdatedAt) || dashboard.LatencyMS != nil {
				t.Fatalf("observation failure changed lifecycle: %+v", dashboard)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOpenClawDashboardPreservesLifecycleWork(t *testing.T) {
	for _, state := range []string{"planned", "provisioning", "error", "deletion_scheduled", "deletion-requested", "setup-error"} {
		t.Run(state, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			instance := statusInstance()
			instance.Status = state
			switch state {
			case "deletion_scheduled":
				instance.DeletionScheduledAt = &instance.UpdatedAt
			case "deletion-requested":
				instance.Status = "running"
				instance.DeletionRequestedAt = &instance.UpdatedAt
			case "setup-error":
				instance.Status = "missing"
				instance.LastError = "configuration failed"
			}
			expectStatusDashboardReads(mock, instance)
			fleet := &statusFleetStub{result: OpenClawFleetResult{State: "running", Port: 19100}}
			dashboard, err := NewOpenClawService(db, fleet).GetDashboard(context.Background(), 42)
			if err != nil {
				t.Fatal(err)
			}
			if fleet.calls != 0 || dashboard.Instance.Status != instance.Status || dashboard.FleetState != instance.Status || dashboard.Instance.LastError != instance.LastError {
				t.Fatalf("health certified lifecycle work: %+v", dashboard)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOpenClawDashboardConcurrentLifecycleChangeWins(t *testing.T) {
	for _, next := range []string{"stopped", "deletion_scheduled", "provisioning", "deleted"} {
		t.Run(next, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			instance := statusInstance()
			instance.Status = "running"
			expectStatusDashboardReads(mock, instance)
			mock.ExpectQuery(`UPDATE openclaw_instances`).WithArgs("running", 19100, nil, instance.ID, int64(42), instance.Status, instance.UpdatedAt).WillReturnError(sql.ErrNoRows)
			query := mock.ExpectQuery(`FROM openclaw_instances WHERE id = \$1 AND user_id = \$2`).WithArgs(instance.ID, int64(42))
			current := instance
			current.Status, current.UpdatedAt = next, instance.UpdatedAt.Add(time.Second)
			if next == "deletion_scheduled" {
				current.DeletionScheduledAt = &current.UpdatedAt
			}
			if next == "deleted" {
				query.WillReturnError(sql.ErrNoRows)
			} else {
				query.WillReturnRows(statusInstanceRows(current))
			}
			fleet := &statusFleetStub{result: OpenClawFleetResult{State: "running", Port: 19100}}
			dashboard, err := NewOpenClawService(db, fleet).GetDashboard(context.Background(), 42)
			if next == "deleted" {
				if !errors.Is(err, ErrOpenClawInstanceNotFound) {
					t.Fatalf("deleted result = %v", err)
				}
			} else if err != nil || dashboard.Instance.Status != next || dashboard.FleetState != next || dashboard.LatencyMS != nil {
				t.Fatalf("stale observation overwrote %s: %+v, %v", next, dashboard, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOpenClawDashboardTelemetryWriteFailureIsUnavailable(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	instance := statusInstance()
	expectStatusDashboardReads(mock, instance)
	mock.ExpectQuery(`UPDATE openclaw_instances`).WillReturnError(errors.New("database unavailable"))
	fleet := &statusFleetStub{result: OpenClawFleetResult{State: "running", Port: 19100}}
	dashboard, err := NewOpenClawService(db, fleet).GetDashboard(context.Background(), 42)
	if err != nil || dashboard.FleetState != "unavailable" || dashboard.Instance.Status != instance.Status {
		t.Fatalf("failed save reported as current: %+v, %v", dashboard, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type statusRoundTripper func(*http.Request) (*http.Response, error)

func (f statusRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOpenClawHTTPStatusHasIndependentDeadline(t *testing.T) {
	client := NewHTTPOpenClawFleetClient("http://bridge.example", strings.Repeat("x", 32))
	client.Client.Transport = statusRoundTripper(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 10*time.Second || time.Until(deadline) < 9*time.Second {
			t.Fatal("status deadline missing or wrong")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"state":"running","port":19100}`)), Header: make(http.Header)}, nil
	})
	if _, err := client.Status(context.Background(), "u42-0123456789ab"); err != nil {
		t.Fatal(err)
	}
	if client.Client.Timeout != 5*time.Minute {
		t.Fatal("status changed lifecycle timeout")
	}
}
