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

if [[ "${1:-}" == "--static-only" ]]; then
	echo
else
	echo
	echo "== Go guard tests"
	GO_TESTS='^Test(ResolveBillingSubscription|SubscriptionBillingGroup|AdminService_CreateUser_BalanceOverflow|APIKeyAuthSnapshotBalanceOverflow|UpdateGroup_SubscriptionSourceAny|ValidateSubscriptionSharing|UpdateGroupNormalizesSubscriptionSharing|CheckBillingEligibilityShared|BuildUsageBillingCommandSharing|FinalizePostUsageBillingUsesSubscriptionGroupID|APIKeyAuth(Shared|Overflow|UsageEndpoint|AdminDisallow|SourceAny))'
	# Deleting guard tests must fail the guard too, not just pass with fewer tests.
	MIN_GO_TESTS=24
	go_log="$(mktemp)"
	(cd backend && go test -tags=unit -count=1 -v -run "$GO_TESTS" ./internal/service/ ./internal/server/middleware/) >"$go_log" 2>&1
	go_status=$?
	passed="$(grep -cE '^--- PASS: Test' "$go_log" || true)"
	if [[ $go_status -ne 0 ]]; then
		grep -E '^(--- FAIL|FAIL|panic:)|_test\.go:[0-9]+:' "$go_log" | head -40
		fail "Go guard tests pass"
	elif [[ "${passed:-0}" -lt $MIN_GO_TESTS ]]; then
		fail "Go guard tests: $passed passed, expected at least $MIN_GO_TESTS (tests removed or renamed?)"
	else
		ok "Go guard tests ($passed passed)"
	fi
	rm -f "$go_log"

	echo
	echo "== Frontend guard specs"
	if (cd frontend && pnpm exec vitest run \
		src/views/admin/__tests__/GroupsView.subscriptionSharing.spec.ts \
		src/components/admin/user/__tests__/UserEditModal.spec.ts \
		src/components/user/__tests__/SubscriptionBalanceOverflowCard.spec.ts); then
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
