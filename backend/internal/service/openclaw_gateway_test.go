package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"
)

type gatewayFleetStub struct {
	OpenClawFleetClient
	tenant string
	calls  int
}

func (s *gatewayFleetStub) Enabled() bool { return true }
func (s *gatewayFleetStub) Dashboard(_ context.Context, tenant string) (OpenClawFleetResult, error) {
	s.tenant = tenant
	s.calls++
	return OpenClawFleetResult{BrowserURL: "https://owner.gateway.example/#native-bootstrap"}, nil
}
func (s *gatewayFleetStub) GatewayDevices(_ context.Context, tenant string, _ openclawgateway.Request) (OpenClawFleetResult, error) {
	s.tenant = tenant
	s.calls++
	return OpenClawFleetResult{Devices: &openclawgateway.Inventory{Pending: []openclawgateway.Device{}, Paired: []openclawgateway.Device{}}}, nil
}

func TestGatewayOperationsEnforceOwnershipAndLifecycle(t *testing.T) {
	for _, operation := range []string{"launch", "devices"} {
		for _, state := range []string{"missing", "stopped", "deletion", "ready"} {
			t.Run(operation+"/"+state, func(t *testing.T) {
				t.Setenv("OPENCLAW_GATEWAY_ENABLED", "1")
				t.Setenv("OPENCLAW_GATEWAY_USER_IDS", "42")
				db, mock, err := sqlmock.New()
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				query := mock.ExpectQuery(`FROM openclaw_instances WHERE id = \$1 AND user_id = \$2`).WithArgs(int64(7), int64(42))
				if state == "missing" {
					query.WillReturnError(sql.ErrNoRows)
				} else {
					var deletion any
					status := state
					if state == "deletion" {
						status = "ready"
						deletion = time.Now().Add(time.Hour)
					}
					now := time.Now()
					query.WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_slug", "name", "status", "worker_host", "port", "last_error", "created_at", "updated_at", "deletion_requested_at", "deletion_scheduled_at"}).AddRow(int64(7), "u42-0123456789ab", "private", status, "default", 19100, "", now, now, nil, deletion))
				}
				fleet := &gatewayFleetStub{}
				if state == "ready" {
					mock.ExpectQuery(`SELECT i.user_id FROM openclaw_instances i`).WithArgs("u42-0123456789ab").WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(int64(42)))
				}
				s := NewOpenClawService(db, fleet)
				if operation == "launch" {
					_, err = s.GetGatewayLaunchURL(context.Background(), 42, 7)
				} else {
					_, err = s.GatewayDevices(context.Background(), 42, 7, openclawgateway.Request{Action: "list"})
				}
				if state == "ready" {
					if err != nil || fleet.calls != 1 || fleet.tenant != "u42-0123456789ab" {
						t.Fatal("did not resolve the owner's actual cell")
					}
				} else if err == nil || fleet.calls != 0 {
					t.Fatal("denied cell reached private bridge")
				}
				if state == "missing" && !errors.Is(err, ErrOpenClawInstanceNotFound) {
					t.Fatal("wrong-owner lookup returned the wrong error")
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
