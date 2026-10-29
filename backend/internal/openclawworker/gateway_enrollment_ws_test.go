package openclawworker

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"
)

func testEnrollmentIdentity() (string, string) {
	key := []byte(strings.Repeat("k", 32))
	publicKey := base64.RawURLEncoding.EncodeToString(key)
	digest := sha256.Sum256(key)
	return hex.EncodeToString(digest[:]), publicKey
}

func TestObserveEnrollmentConnectPreservesSignedFieldsAndCapturesIdentity(t *testing.T) {
	deviceID, publicKey := testEnrollmentIdentity()
	obs := &enrollmentObservation{enrollID: "enrollment-12345678"}
	input := map[string]any{"type": "req", "method": "connect", "params": map[string]any{"role": "operator", "scopes": []string{"operator.read"}, "device": map[string]any{"id": deviceID, "publicKey": publicKey}, "auth": map[string]any{"token": "ticket"}}}
	payload, _ := json.Marshal(input)
	output, changed := observeEnrollmentConnect(payload, "ticket", obs)
	if !changed || !bytes.Equal(output, payload) {
		t.Fatal("signed connect frame was modified")
	}
	if !obs.haveKey || obs.binding.DeviceID != deviceID || obs.binding.PublicKey != publicKey {
		t.Fatal("native identity was not captured")
	}
}

func TestObserveEnrollmentConnectAcceptsIOSBootstrapToken(t *testing.T) {
	deviceID, publicKey := testEnrollmentIdentity()
	obs := &enrollmentObservation{enrollID: "enrollment-12345678"}
	input := map[string]any{"type": "req", "method": "connect", "params": map[string]any{
		"role": "operator", "scopes": []string{"operator.read"},
		"client": map[string]any{"id": "openclaw-ios"},
		"device": map[string]any{"id": deviceID, "publicKey": publicKey},
		"auth":   map[string]any{"bootstrapToken": "ticket"},
	}}
	payload, _ := json.Marshal(input)
	if _, changed := observeEnrollmentConnect(payload, "ticket", obs); !changed || !obs.haveKey {
		t.Fatal("iOS bootstrap ticket was not observed")
	}
}

func TestObserveEnrollmentPairEventCapturesExactRequest(t *testing.T) {
	deviceID, publicKey := testEnrollmentIdentity()
	obs := &enrollmentObservation{enrollID: "enrollment-12345678", binding: openclawgateway.EnrollmentBinding{DeviceID: deviceID, PublicKey: publicKey, Roles: []string{"operator"}, Scopes: []string{"operator.read"}}, haveKey: true}
	observeEnrollmentPairEvent([]byte(`{"type":"event","event":"device.pair.requested","payload":{"requestId":"request-12345678","deviceId":"`+deviceID+`","publicKey":"`+publicKey+`"}}`), obs)
	if !obs.haveReq || obs.binding.RequestID != "request-12345678" {
		t.Fatal("pair request identity was not captured")
	}
}

func TestBindEnrollmentObservationRequiresSignedIdentityAndRequest(t *testing.T) {
	deviceID, publicKey := testEnrollmentIdentity()
	obs := &enrollmentObservation{enrollID: "enrollment-12345678", binding: openclawgateway.EnrollmentBinding{DeviceID: deviceID, PublicKey: publicKey, RequestID: "request-12345678", Roles: []string{"operator"}, Scopes: []string{"operator.read"}}, haveKey: true, haveReq: true}
	w := &Worker{}
	w.bindEnrollmentObservation("u42-123456789abc", obs)
	w.enrollmentMu.RLock()
	got := w.enrollmentByDevice["u42-123456789abc/"+deviceID]
	w.enrollmentMu.RUnlock()
	if got.EnrollmentID != obs.enrollID || got.RequestID != obs.binding.RequestID {
		t.Fatal("validated enrollment binding was not stored")
	}
}

func TestObserveEnrollmentRejectsUnrelatedAndReplacementRequests(t *testing.T) {
	deviceID, publicKey := testEnrollmentIdentity()
	obs := &enrollmentObservation{binding: openclawgateway.EnrollmentBinding{DeviceID: deviceID, PublicKey: publicKey}, haveKey: true}
	for _, invalid := range []string{
		`{"event":"device.pair.requested","payload":{"requestId":"request-12345678","deviceId":"another-device","publicKey":"` + publicKey + `"}}`,
		`{"event":"device.pair.requested","payload":{"requestId":"request-12345678","deviceId":"` + deviceID + `"}}`,
		`{"event":"device.pair.requested","payload":{"requestId":"request-12345678","publicKey":"` + publicKey + `"}}`,
	} {
		observeEnrollmentPairEvent([]byte(invalid), obs)
		if obs.haveReq {
			t.Fatal("unrelated native event bound an enrollment")
		}
	}
	event := func(id string) []byte {
		b, _ := json.Marshal(map[string]any{"event": "device.pair.requested", "payload": map[string]any{"requestId": id, "deviceId": deviceID, "publicKey": publicKey}})
		return b
	}
	observeEnrollmentPairEvent(event("request-original"), obs)
	observeEnrollmentPairEvent(event("request-replaced"), obs)
	if obs.binding.RequestID != "request-original" {
		t.Fatal("replacement request overwrote the reviewed request")
	}
}

func TestBindEnrollmentObservationDoesNotReplaceCompetingEnrollment(t *testing.T) {
	deviceID, publicKey := testEnrollmentIdentity()
	w := &Worker{}
	for _, id := range []string{"enrollment-original", "enrollment-competing"} {
		obs := &enrollmentObservation{enrollID: id, binding: openclawgateway.EnrollmentBinding{DeviceID: deviceID, PublicKey: publicKey, RequestID: "request-12345678"}, haveKey: true, haveReq: true}
		w.bindEnrollmentObservation("u42-123456789abc", obs)
	}
	if w.enrollmentByDevice["u42-123456789abc/"+deviceID].EnrollmentID != "enrollment-original" {
		t.Fatal("competing scan replaced the first enrollment")
	}
}
