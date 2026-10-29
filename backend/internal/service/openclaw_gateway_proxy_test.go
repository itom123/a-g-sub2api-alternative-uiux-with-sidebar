package service

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestGatewayProxySeparatesNativeAndManagementIdentity(t *testing.T) {
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "1")
	t.Setenv("OPENCLAW_GATEWAY_USER_IDS", "42")
	t.Setenv("OPENCLAW_GATEWAY_PUBLIC_ORIGIN_TEMPLATE", "https://{tenant}.claw.example")
	tenant := "u42-0123456789ab"
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("m", 40) || r.Header.Get("X-ClawHub-Native-Authorization") != "Bearer native-device" {
			t.Error("credential boundaries changed")
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("Forwarded") != "" {
			t.Error("forwarded customer identities")
		}
		if r.URL.Path != "/v1/gateway/"+tenant+"/assets/app.js" || r.Host != tenant+".claw.example" {
			t.Error("selected the wrong native route")
		}
		_, _ = w.Write([]byte("native-content"))
	}))
	defer bridge.Close()
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()
	mock.ExpectQuery(`SELECT i.user_id FROM openclaw_instances i`).WithArgs(tenant).WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(int64(42)))
	s := NewOpenClawService(db, NewHTTPOpenClawFleetClient(bridge.URL, strings.Repeat("m", 40)))
	r := httptest.NewRequest("GET", "https://"+tenant+".claw.example/assets/app.js", nil)
	r.Header.Set("Authorization", "Bearer native-device")
	r.Header.Set("X-ClawHub-Native-Authorization", "Bearer forged-operator")
	r.Header.Set("Cookie", "platform-session=secret")
	r.Header.Set("X-Forwarded-For", "127.0.0.1")
	r.Header.Set("Forwarded", "for=127.0.0.1")
	w := httptest.NewRecorder()
	s.ProxyGateway(w, r, tenant)
	body, _ := io.ReadAll(w.Result().Body)
	if w.Code != 200 || string(body) != "native-content" {
		t.Fatalf("proxy failed: %d", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGatewayProxyDeniesWrongOriginAndWithdrawnOwner(t *testing.T) {
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "1")
	t.Setenv("OPENCLAW_GATEWAY_USER_IDS", "42")
	t.Setenv("OPENCLAW_GATEWAY_PUBLIC_ORIGIN_TEMPLATE", "https://{tenant}.claw.example")
	for _, state := range []string{"origin", "withdrawn", "other-owner"} {
		t.Run(state, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer func() { _ = db.Close() }()
			tenant := "u42-0123456789ab"
			r := httptest.NewRequest("GET", "https://"+tenant+".claw.example/", nil)
			if state == "origin" {
				r.Header.Set("Origin", "https://u1-abcdef012345.claw.example")
			} else {
				query := mock.ExpectQuery(`SELECT i.user_id FROM openclaw_instances i`).WithArgs(tenant)
				if state == "withdrawn" {
					query.WillReturnError(sql.ErrNoRows)
				} else {
					query.WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(int64(43)))
				}
			}
			w := httptest.NewRecorder()
			NewOpenClawService(db, nil).ProxyGateway(w, r, tenant)
			if w.Code != 404 {
				t.Fatal("unauthorized route remained available")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
