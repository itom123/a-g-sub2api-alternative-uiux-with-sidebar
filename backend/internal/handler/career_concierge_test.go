package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCareerInternalEndpointsRequireBridgeToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, configured := range []string{"", "expected-secret"} {
		t.Setenv("OPENCLAW_CONCIERGE_INTERNAL_TOKEN", configured)
		for _, method := range []string{"pair", "profile"} {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			ctx.Request.Header.Set("X-OpenClaw-Concierge-Token", "wrong-secret")
			h := &OpenClawHandler{}
			if method == "pair" {
				h.PairCareerTelegram(ctx)
			} else {
				h.GetCareerInternalProfile(ctx)
			}
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("%s returned %d", method, recorder.Code)
			}
		}
	}
}
