#!/usr/bin/env bash
# Fork guard: fails when fork customizations (docs/fork/CUSTOMIZATIONS.md) were dropped or overridden,
# typically by an upstream sync. Usage: tools/fork-guard.sh [--static-only]
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

failures=()
fail() { failures+=("$1"); echo "FAIL: $1"; }
ok() { echo "ok:   $1"; }

# require_text FILE PATTERN DESCRIPTION: fixed-string match
require_text() {
	if [[ ! -f "$1" ]]; then
		fail "$3 (missing file $1)"
	elif grep -qF -- "$2" "$1"; then
		ok "$3"
	else
		fail "$3 ($1 lacks '$2')"
	fi
}

# Migration checksum as the runner records it: sha256 of the trimmed file content.
PYTHON=""
for candidate in python3 python; do
	if command -v "$candidate" >/dev/null 2>&1 && "$candidate" -c "" >/dev/null 2>&1; then
		PYTHON="$candidate"
		break
	fi
done
migration_checksum() {
	"$PYTHON" - "$1" <<'PY'
import hashlib, sys
data = open(sys.argv[1], "rb").read().decode("utf-8").strip()
print(hashlib.sha256(data.encode("utf-8")).hexdigest())
PY
}

echo "== Static invariants"

declare -A MIGRATIONS=(
	["262_group_subscription_sharing.sql"]="efd61ba212d548803dfbffd0d7b5b64afc438e02ef3857da4bbe1e862db03617"
	["263_balance_overflow_user_and_any_source.sql"]="f39edab9326cad0f92b9f96e615fe236f6e5d4caaa04cd07ca138402a206bbd5"
)
for name in "${!MIGRATIONS[@]}"; do
	path="backend/migrations/$name"
	if [[ ! -f "$path" ]]; then
		fail "migration $name present"
		continue
	fi
	actual="$(migration_checksum "$path")"
	if [[ "$actual" == "${MIGRATIONS[$name]}" ]]; then
		ok "migration $name unchanged"
	else
		fail "migration $name checksum changed (applied in production; never edit it)"
	fi
done

repo=backend/internal/repository/api_key_repo.go
require_text "$repo" "user.FieldBalanceOverflowAdminAllowed" "auth projection selects users.balance_overflow_admin_allowed"
require_text "$repo" "user.FieldBalanceOverflowEnabled" "auth projection selects users.balance_overflow_enabled"
require_text "$repo" "group.FieldSubscriptionSourceGroupIds" "auth projection selects groups.subscription_source_group_ids"
require_text "$repo" "group.FieldSubscriptionSourceAny" "auth projection selects groups.subscription_source_any"

for mw in api_key_auth.go api_key_auth_google.go; do
	require_text "backend/internal/server/middleware/$mw" "ResolveBillingSubscription(" "$mw resolves the billing subscription"
done

cache=backend/internal/service/api_key_auth_cache_impl.go
for field in BalanceOverflowAdminAllowed BalanceOverflowEnabled SubscriptionSourceAny SubscriptionSourceGroupIDs; do
	count="$(grep -c "$field:" "$cache" 2>/dev/null || true)"
	if [[ "${count:-0}" -ge 2 ]]; then
		ok "auth snapshot carries $field both ways"
	else
		fail "auth snapshot carries $field both ways ($cache has $count mapping(s))"
	fi
done

for f in billing_cache_service.go billing_inflight_reservation.go gateway_usage_billing.go openai_gateway_usage.go; do
	require_text "backend/internal/service/$f" "SubscriptionBillingGroup(" "$f decides billing mode with SubscriptionBillingGroup"
done
require_text backend/internal/handler/gateway_inflight_reservation.go "SubscriptionBillingGroup(" "handler reservation decides billing mode with SubscriptionBillingGroup"
require_text backend/internal/service/subscription_service.go "subListCacheKey(" "subscription cache invalidation drops the per-user list"
require_text backend/internal/repository/user_repo.go "SetBalanceOverflowEnabled(true)" "new users start with balance overflow on"

require_text frontend/src/views/user/SubscriptionsView.vue "SubscriptionBalanceOverflowCard" "user subscriptions page shows the overflow switch"
require_text frontend/src/views/admin/GroupsView.vue "subscription_source_any" "admin group form has the any-subscription switch"
require_text frontend/src/views/admin/GroupsView.vue "subscription_source_group_ids" "admin group form has the subscription source picker"
require_text frontend/src/components/admin/user/UserEditModal.vue "balance_overflow_admin_allowed" "admin user form has the overflow permission"

# Fork-owned source files of the features restored on 2026-11-04 (docs/fork/CUSTOMIZATIONS.md, sections 2-6).
for f in 	backend/internal/service/payment_session_cancel.go 	backend/internal/service/payment_subscription_policy.go 	backend/internal/service/ops_job_health.go 	backend/internal/service/ops_gateway_overhead.go 	backend/internal/service/ops_request_timing.go 	backend/internal/service/ops_context_limit_error.go 	backend/internal/pkg/usagestats/user_insights.go 	backend/internal/repository/usage_log_repo_user_insights.go 	frontend/src/views/user/subscriptionCheckout.ts 	frontend/src/utils/usageDisplay.ts 	frontend/src/components/charts/trendComparison.ts 	frontend/src/components/user/dashboard/UserDashboardSubscriptions.vue 	frontend/src/components/admin/subscription/SubscriptionUsageCell.vue; do
	if [[ -f "$f" ]]; then ok "present: $f"; else fail "fork-owned file present: $f"; fi
done

# 9. OpenClaw worker cell status and WeChat login (docs/fork/CUSTOMIZATIONS.md, section 9).
ocw=backend/internal/openclawworker
if [[ -f "$ocw/cell_status.go" ]]; then ok "present: $ocw/cell_status.go"; else fail "fork-owned file present: $ocw/cell_status.go"; fi
require_text "$ocw/cell_status.go" "func (w *Worker) cellStatus(" "worker has the cell status action"
require_text "$ocw/cell_status.go" "func (w *Worker) inspectCell(" "worker inspects cell containers in one place"
require_text "$ocw/cell_status.go" "func parseCellObservation(" "worker parses cell observations in one place"
require_text "$ocw/gateway_proxy.go" "w.inspectCell(" "gateway proxy shares the cell inspection"
require_text "$ocw/worker.go" "cellStatus(" "worker dispatches the status action"
require_text "$ocw/worker.go" "finishWeChatWait(" "WeChat login finishes through finishWeChatWait"
require_text "$ocw/worker.go" "startWeChatChannelIfStopped(" "WeChat login restarts a stopped channel without a gateway reload"

# 7. No upstream tutorial or branding (docs/fork/CUSTOMIZATIONS.md, section 7).
for f in frontend/src/composables/useOnboardingTour.ts frontend/src/stores/onboarding.ts frontend/src/components/Guide frontend/src/styles/onboarding.css frontend/src/components/common/ProxyAdBanner.vue; do
	if [[ -e "$f" ]]; then fail "tutorial and upstream ad removed ($f is back)"; else ok "absent: $f"; fi
done
if grep -qF '"driver.js"' frontend/package.json; then fail "tutorial dependency driver.js removed"; else ok "no driver.js dependency"; fi
for d in frontend/src/i18n/locales docs/legal; do
	hits="$(grep -rli 'sub2api' "$d" 2>/dev/null || true)"
	if [[ -n "$hits" ]]; then fail "no Sub2API wording in $d ($(echo $hits))"; else ok "no Sub2API wording in $d"; fi
done
require_text backend/internal/service/setting_features.go 'DefaultSiteName = "AG_>"' "default site name is AG_>"
require_text frontend/src/utils/branding.ts "DEFAULT_SITE_NAME = 'AG_>'" "frontend default site name is AG_>"
# The deploy tooling parses the binary's --version line; it must keep the upstream format.
require_text backend/cmd/server/main.go 'log.Printf("Sub2API %s (commit: %s, built: %s)' "binary --version keeps the format deploy/production parses"

# 8. Referral codes admit new users in invitation-only mode (docs/fork/CUSTOMIZATIONS.md, section 8).
require_text backend/internal/service/auth_service.go "s.resolveSignupInvitation(ctx, invitationCode, affiliateCode, s.loadSignupInvitation)" "email sign-up accepts referral codes as invitations"
require_text backend/internal/service/auth_oauth_email_flow.go "s.resolveSignupInvitation(ctx, invitationCode, affiliateCode, s.loadOAuthRegistrationInvitation)" "OAuth sign-up accepts referral codes as invitations"
require_text backend/internal/handler/auth_handler.go "h.authService.IsAffiliateInvitationCode(" "invitation check endpoint accepts referral codes"
require_text frontend/src/views/auth/RegisterView.vue "prefillInvitationFromReferral(syncAffiliateReferralCode())" "register page fills the invitation field from the invite link"

if [[ "${1:-}" == "--static-only" ]]; then
	echo
else
	echo
	echo "== Go guard tests"
	# Every Test function in these files must exist and pass. Deleting a file or a test fails the guard.
	GO_GUARD_FILES=(
		# 1. Subscription billing
		backend/internal/service/balance_overflow_test.go
		backend/internal/service/balance_overflow_plumbing_test.go
		backend/internal/service/subscription_sharing_test.go
		backend/internal/service/admin_group_sharing_test.go
		backend/internal/service/billing_cache_service_sharing_test.go
		backend/internal/service/gateway_usage_billing_sharing_test.go
		backend/internal/server/middleware/api_key_auth_sharing_test.go
		# 2. Payments
		backend/internal/service/payment_session_cancel_test.go
		backend/internal/service/payment_one_time_policy_test.go
		# 3. Ops monitor and gateway overhead
		backend/internal/service/ops_health_score_test.go
		backend/internal/service/ops_job_health_test.go
		backend/internal/service/ops_gateway_overhead_test.go
		backend/internal/service/ops_request_timing_test.go
		backend/internal/handler/ops_error_logger_context_limit_test.go
		backend/internal/handler/ops_gateway_overhead_middleware_test.go
		# 4. Claude bridge failover
		backend/internal/service/openai_gateway_messages_failover_test.go
		# 5. Customer dashboard insights
		backend/internal/service/usage_service_user_insights_test.go
		backend/internal/pkg/usagestats/user_insights_test.go
		backend/internal/handler/usage_handler_user_insights_test.go
		# 6. OpenClaw pricing
		backend/internal/service/openclaw_billing_lock_test.go
		# 8. Referral codes as invitations
		backend/internal/service/auth_service_referral_invitation_test.go
		# 9. OpenClaw worker cell status and WeChat login
		backend/internal/openclawworker/worker_status_test.go
		backend/internal/openclawworker/gateway_proxy_test.go
		backend/internal/openclawworker/worker_test.go
	)
	declare -A pkg_tests=()
	expected=0
	for f in "${GO_GUARD_FILES[@]}"; do
		if [[ ! -f "$f" ]]; then
			fail "guard test file present: $f"
			continue
		fi
		names="$(grep -oE '^func (Test[A-Za-z0-9_]+)' "$f" | sed 's/^func //')"
		if [[ -z "$names" ]]; then
			fail "guard test file has tests: $f"
			continue
		fi
		pkg="./$(dirname "${f#backend/}")/"
		for n in $names; do
			pkg_tests[$pkg]+="${pkg_tests[$pkg]:+|}$n"
			expected=$((expected + 1))
		done
	done
	go_log="$(mktemp)"
	go_status=0
	for pkg in "${!pkg_tests[@]}"; do
		(cd backend && go test -tags=unit -count=1 -v -run "^(${pkg_tests[$pkg]})\$" "$pkg") >>"$go_log" 2>&1 || go_status=1
	done
	passed="$(grep -cE '^--- PASS: Test' "$go_log" || true)"
	if [[ $go_status -ne 0 ]]; then
		grep -E '^(--- FAIL|FAIL|panic:)|_test\.go:[0-9]+:' "$go_log" | head -40
		fail "Go guard tests pass"
	elif [[ "${passed:-0}" -lt $expected ]]; then
		fail "Go guard tests: $passed passed, expected $expected (a guard test was skipped or renamed?)"
	else
		ok "Go guard tests ($passed passed)"
	fi
	rm -f "$go_log"

	echo
	echo "== Frontend guard specs"
	if (cd frontend && pnpm exec vitest run \
		src/views/admin/__tests__/GroupsView.subscriptionSharing.spec.ts \
		src/components/admin/user/__tests__/UserEditModal.spec.ts \
		src/components/user/__tests__/SubscriptionBalanceOverflowCard.spec.ts \
		src/views/user/__tests__/subscriptionCheckout.spec.ts \
		src/views/user/__tests__/PaymentView.spec.ts \
		src/components/payment/__tests__/PaymentStatusPanel.spec.ts \
		src/views/admin/ops/components/__tests__/OpsDashboardHeader.spec.ts \
		src/components/user/dashboard/insights/__tests__/aggregate.spec.ts \
		src/components/user/dashboard/__tests__/UserDashboardSubscriptions.spec.ts \
		src/views/admin/__tests__/SubscriptionsView.usageColumns.spec.ts \
		src/views/auth/__tests__/RegisterView.spec.ts); then
		ok "frontend guard specs"
	else
		fail "frontend guard specs pass"
	fi
	echo
fi

if [[ ${#failures[@]} -gt 0 ]]; then
	echo "fork-guard: ${#failures[@]} failure(s). A fork customization was dropped or changed; see docs/fork/CUSTOMIZATIONS.md."
	for f in "${failures[@]}"; do echo "  - $f"; done
	exit 1
fi
echo "fork-guard: all fork customizations intact."
