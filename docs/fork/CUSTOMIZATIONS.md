# Fork customizations registry

This repository is a fork of [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api). The features below are fork-only.
When upstream is merged in (see [UPSTREAM_SYNC.md](UPSTREAM_SYNC.md)), every item here must survive. The
`fork-guard` CI job (`tools/fork-guard.sh`) checks the invariants and runs the guard tests, so a sync that drops or
overrides one of them fails before it can be merged.

How to read each entry:
- **Fork-owned files** exist only in this fork. Keep them whole when merging upstream.
- **Hook points** are the small edits in upstream-owned files that wire the feature in. A conflict in one of these
  files means: take upstream's change and re-apply the hook, never take either side wholesale.

---

## 1. Subscription sharing and subscription-first billing

**What it does.** A subscriber's daily/weekly/monthly allowance is always charged first. Once it is used up, the
balance pays only when the user's balance overflow is allowed: admin permission AND the user's own switch, both on by
default, and the balance above the auth threshold. Otherwise the usage-limit error (429) stands. Standard
(non-subscription) groups can draw on subscription quota in one of two ways:
- `subscription_source_any`: any of the user's active subscriptions, earliest expiry first;
- `subscription_source_group_ids`: an ordered list of subscription groups.

A request admitted while quota remains is charged fully to the subscription, so the quota boundary is not split.

**Data (migrations):**
- `262_group_subscription_sharing.sql`: `groups.subscription_source_group_ids` (jsonb, default `[]`) and
  `groups.subscription_overflow_to_balance` (bool, default false). The second column is kept for compatibility and is
  no longer read by billing.
- `263_balance_overflow_user_and_any_source.sql`: `users.balance_overflow_admin_allowed` and
  `users.balance_overflow_enabled` (bool, default true), and `groups.subscription_source_any` (bool, default false).

Both migrations are applied in production. Never edit them: the runner verifies checksums (see
`tools/fork-guard.sh` for the recorded values).

**Fork-owned files:**
- `backend/internal/service/subscription_billing.go`: `SubscriptionBillingGroup`, `ResolveBillingSubscription`, the
  per-user subscription list cache.
- `frontend/src/components/user/SubscriptionBalanceOverflowCard.vue`: the user switch on 我的订阅.
- Tests: `backend/internal/service/{balance_overflow_test,balance_overflow_plumbing_test,subscription_sharing_test,admin_group_sharing_test,billing_cache_service_sharing_test,gateway_usage_billing_sharing_test}.go`,
  `backend/internal/server/middleware/api_key_auth_sharing_test.go`,
  `frontend/src/components/user/__tests__/SubscriptionBalanceOverflowCard.spec.ts`,
  `frontend/src/views/admin/__tests__/GroupsView.subscriptionSharing.spec.ts`.

**Hook points in upstream-owned files:**

| File | Hook |
| --- | --- |
| `backend/internal/server/middleware/api_key_auth.go`, `api_key_auth_google.go` | Subscription resolution is one call to `ResolveBillingSubscription(ctx, apiKey.User, apiKey.Group, balanceOK)`. `/v1/usage` stays display-only (plain `GetActiveSubscription`). |
| `backend/internal/service/billing_cache_service.go`, `billing_inflight_reservation.go`, `gateway_usage_billing.go`, `openai_gateway_usage.go`, `backend/internal/handler/gateway_inflight_reservation.go` | The billing mode is `SubscriptionBillingGroup(keyGroup, sub) != nil`, never `group.IsSubscriptionType() && sub != nil`. Limits and the usage cache key come from the subscription's group (`p.Subscription.GroupID`), not the API key's group. |
| `backend/internal/service/subscription_service.go` | `InvalidateSubCache*` also drops the per-user list key `subs:<user>`. |
| `backend/internal/repository/api_key_repo.go` (`GetByKeyForAuth`) | The user `Select` includes both balance-overflow columns; the group `Select` includes the three subscription-source columns. **A missing column here silently disables the feature in production.** |
| `backend/internal/service/api_key_auth_cache*.go` | The snapshot carries all fields in both directions; the snapshot version was bumped (v27). |
| `backend/internal/repository/user_repo.go` | User creation always writes both switches as true; admin create applies an explicit "off" afterwards. |
| `backend/internal/service/admin_group.go`, `admin_group_duplicate.go`, `admin_user.go`, `user_service.go`, handler DTOs | Validation and normalisation of the group fields; admin and user update paths; DTO exposure. The group fields are admin-only. |
| `backend/ent/schema/{group,user}.go` + generated `backend/ent/**` | Schema fields. After a merge, regenerate ent instead of hand-merging generated code. |
| `frontend/src/views/admin/GroupsView.vue`, `frontend/src/components/admin/user/UserEditModal.vue`, `frontend/src/views/user/SubscriptionsView.vue`, `frontend/src/types/index.ts`, `frontend/src/api/user.ts`, zh/en i18n | The admin switches, the source picker, the user card, and the types and labels. |

**Invariants checked by `fork-guard`:** the migrations are present and unchanged; the auth projection contains the
columns; both middlewares use the resolver; billing uses the predicate; the snapshot carries the fields; the
frontend wiring is present. The behaviour itself is proven by the guard tests listed above.

---

## Adding a new fork customization

1. Put the logic in fork-owned files and keep hooks in upstream files as small as possible.
2. Name new migrations `9NN_fork_<name>.sql` (see UPSTREAM_SYNC.md); never renumber a migration already applied in
   production.
3. Add an entry here, add its invariants and test names to `tools/fork-guard.sh`, and make sure CI is green.
