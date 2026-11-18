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
| `backend/internal/service/api_key_auth_cache*.go` | The snapshot carries all fields in both directions; the snapshot version was bumped (v28 after the v0.2.21 sync). |
| `backend/internal/repository/user_repo.go` | User creation always writes both switches as true; admin create applies an explicit "off" afterwards. |
| `backend/internal/service/admin_group.go`, `admin_group_duplicate.go`, `admin_user.go`, `user_service.go`, handler DTOs | Validation and normalisation of the group fields; admin and user update paths; DTO exposure. The group fields are admin-only. |
| `backend/ent/schema/{group,user}.go` + generated `backend/ent/**` | Schema fields. After a merge, regenerate ent instead of hand-merging generated code. |
| `frontend/src/views/admin/GroupsView.vue`, `frontend/src/components/admin/user/UserEditModal.vue`, `frontend/src/views/user/SubscriptionsView.vue`, `frontend/src/types/index.ts`, `frontend/src/api/user.ts`, zh/en i18n | The admin switches, the source picker, the user card, and the types and labels. |

**Invariants checked by `fork-guard`:** the migrations are present and unchanged; the auth projection contains the
columns; both middlewares use the resolver; billing uses the predicate; the snapshot carries the fields; the
frontend wiring is present. The behaviour itself is proven by the guard tests listed above.

---

## 2–6. Features restored on 2026-11-04

These had been in production since 2026-11-01 to 2026-11-03, but were built from local trees and never committed
here. A deploy from `main` on 2026-11-04 removed them; they were restored by merging the recorded production tree.
None of them adds a migration. Guard: the files named below must exist, and every test in each guard test file must
pass (`GO_GUARD_FILES` and the frontend spec list in `tools/fork-guard.sh`).

| # | Feature | Fork-owned files (examples) | Main hook points in upstream files |
| --- | --- | --- | --- |
| 2 | Payments: cancel/exit an open checkout, one-time and subscription purchase policy | `payment_session_cancel.go`, `payment_subscription_policy.go`, `frontend/src/views/user/subscriptionCheckout.ts` | `payment_handler.go`, `routes/payment.go`, `payment_order*.go`, `PaymentView.vue`, `PaymentStatusPanel.vue`, Stripe/Airwallex views |
| 3 | Ops monitor: per-job health, TTFT/SLA scoring, exact window percentiles, gateway overhead with upload/processing split, context-window errors attributed to the client | `ops_job_health.go`, `ops_gateway_overhead.go`, `ops_request_timing.go`, `ops_context_limit_error.go` | `ops_health_score.go`, `ops_dashboard*.go`, `ops_service.go`, `ops_repo_dashboard.go`, `ops_error_logger.go`, `gateway_handler.go`, ops admin views |
| 4 | Claude bridge failover: hold `message_start`, 400 for context window, `overloaded_error` | (tests) `openai_gateway_messages_failover_test.go` | `openai_gateway_messages.go`, `openai_gateway_handler.go` |
| 5 | Customer dashboard (full): insights, exact aggregates, comparison trends, subscription widgets, admin subscription usage columns | `usagestats/user_insights.go`, `usage_log_repo_user_insights.go`, `frontend/src/components/user/dashboard/insights/*`, `trendComparison.ts`, `usageDisplay.ts`, `SubscriptionUsageCell.vue` | `usage_handler.go`, `usage_service.go`, `DashboardView.vue`, `AppHeader.vue`, `stores/subscriptions.ts` |
| 6 | OpenClaw pricing and billing lock; official pricing | (tests) `openclaw_billing_lock_test.go` | `openclaw.go`, `api_key_service.go`, `pricing_official.go`, `OpenClawFleetView.vue` |

---

## 7. No onboarding tutorial and no upstream branding

**What it does.** The upstream first-use tutorial (driver.js tour) is removed, and everything a user or admin sees
says `AG_>` where upstream said "Sub2API": UI text, site-name fallbacks, emails, TOTP issuer (follows the site name),
passkey display name, Codex model descriptions, copied client configs (`AG_API_KEY`, provider `ag`), admin and API
error messages, and the admin compliance commitment (served in-app at `/legal/admin-compliance`). Default payment
subjects use `AG` without the `_>` symbols, which some payment providers reject or re-escape. Upstream GitHub links
and the upstream proxy advert are gone.

**Deliberately unchanged** (machine identifiers; renaming them breaks deployments, stored data or interop): the Go
module path, database and table names, Redis keys, browser storage keys, the `/v1/sub2api/billing` relay protocol,
the plugin package format, the `sub2api-admin` WebSocket subprotocol, import/export format names, env var names,
the Docker image and binary names, the `--version` output (`Sub2API <version> (commit: <sha>, …)`, parsed by
`deploy/production`), and the OpenClaw provider ID `sub2api` (a config key inside live tenant
workspaces; renaming it needs a migration of those workspaces).

**When merging upstream:** drop any re-added tour files, `driver.js`, `data-tour` attributes and `ProxyAdBanner`;
reword new "Sub2API" strings in the zh/en locales and `docs/legal`. `fork-guard` checks the files stay deleted and
that the locales and legal docs contain no "Sub2API".

---

## 8. Referral codes admit new users in invitation-only mode

**What it does.** Users see their affiliate code as "my invitation code" (我的邀请码) and share it as
`/register?aff=CODE`. Upstream only accepts admin-issued invitation codes when invitation-only registration is on, so
a new user holding a friend's code could not sign up. Here, while the affiliate program is on, an active user's
affiliate code also satisfies the invitation requirement, whether it is typed into the invitation field or carried
by the invite link. That code is bound as the new user's inviter. Admin invitation codes still work and are still
single-use; a referral code is reusable and never consumes one. Codes of disabled users are rejected.

The register page and the OAuth invitation prompts prefill the invitation field with the referral code the visitor
arrived with, and the invitation check endpoint reports a referral code as valid.

The register page never ignores a click. Upstream gave up silently when the invitation or promo code check was still
running or had failed, and treated a check that could not run (the check endpoint's per-IP limit of 10/min, which
visitors behind one shared address exhaust together, or a network error) as "invalid". Here a submit waits for a
check still in flight, re-checks a code that has no definite answer, and stops only on a definite "invalid" answer,
showing the reason inline and as a toast. When the check cannot run, the submit goes ahead and the server, which
enforces the code at registration anyway, decides. A slower answer for an older value never overwrites the current
one. The admin settings show, under the invitation switch, which codes admit new users right now (none while
registration is off; admin codes only while the affiliate program is off; both otherwise).

**Hook points in upstream-owned files:**

| File | Hook |
| --- | --- |
| `backend/internal/service/auth_service.go` | `resolveSignupInvitation` / `affiliateInvitationCode` / `IsAffiliateInvitationCode`; `RegisterWithVerification` and `loginOrRegisterOAuthWithTokenPair` call `resolveSignupInvitation` instead of checking the redeem code inline. |
| `backend/internal/service/auth_oauth_email_flow.go`, `auth_email_oauth_auto.go` | `validateOAuthRegistrationInvitation` takes the affiliate code, delegates to `resolveSignupInvitation` and returns the code to bind; `FinalizeOAuthEmailAccount` and `createEmailOAuthUser` bind that code. |
| `backend/internal/service/affiliate_service.go` | `LookupInviterByCode`. |
| `backend/internal/handler/auth_handler.go` (`ValidateInvitationCode`) | Falls back to `IsAffiliateInvitationCode`. |
| `frontend/src/views/auth/RegisterView.vue`, `PendingOAuthCreateAccountForm.vue`, the OAuth callback views, `frontend/src/utils/oauthAffiliate.ts`, zh/en i18n | Prefill via `loadReferralInvitationCode`; the `auth.invitationCodeReferralHint` hint. |
| `frontend/src/views/auth/RegisterView.vue` | `handleRegister` → `codesAllowSubmit` (`settlePromoCheck` / `settleInvitationCheck`) → `submitRegistration`; `stopSubmit` and the `register-error` banner; the `unavailable` validation state; `createLatestCheck`. Keep these over upstream's early returns. |
| `frontend/src/views/auth/EmailVerifyView.vue` | The `email-verify-error` banner; localized `INVITATION_CODE_*` errors. |
| `frontend/src/views/admin/SettingsView.vue`, zh/en `admin/settings.ts` | `invitationCodeStatus` and the `invitation-code-status` line under the invitation switch. |

**Invariants checked by `fork-guard`:** the hooks above are present, and the tests in
`backend/internal/service/auth_service_referral_invitation_test.go` and
`frontend/src/views/auth/__tests__/RegisterView.spec.ts` pass.

---

## 9. OpenClaw worker cell status and WeChat login

**What it does.** The OpenClaw worker (`backend/cmd/openclaw-worker`) answers a `status` action for a tenant's cell:
container state, identity, loopback gateway mapping and gateway health latency, from one `podman inspect`. The gateway
proxy uses the same inspection, so what the status reports is what the proxy would dial. This has run on the uk-02
worker since 2026-11-01. The worker's WeChat login (`finishWeChatWait`, `startWeChatChannelIfStopped`) restarts a
stopped channel without reloading the gateway.

**Fork-owned files:** `backend/internal/openclawworker/cell_status.go` and `worker_status_test.go`.

**Hook points in shared files:**

| File | Hook |
| --- | --- |
| `backend/internal/openclawworker/worker.go` | The `status` action calls `cellStatus`; WeChat login goes through `finishWeChatWait` and `startWeChatChannelIfStopped`. |
| `backend/internal/openclawworker/gateway_proxy.go` | `gatewayCell` and `parseGatewayCell` delegate to `inspectCell` and `parseCellObservation`. |

**Invariants checked by `fork-guard`:** `cell_status.go` and the functions above are present, and the tests in
`worker_status_test.go`, `gateway_proxy_test.go` and `worker_test.go` pass.

---

## 10. OpenClaw models follow the billing group; truthful service counts

**What it does.** The models a workspace offers come from its billing group's model allowlist, which the administrator
edits in Group Management (`openClawSupportedModels`). Exact entries keep the administrator's order; `*` entries expand
against the built-in list; a group whose allowlist is off offers the built-in list, and so does an allowlist naming no
model a cell can offer (reported as `fallback`, so a workspace is never blocked over it). The default is the group's
`default_mapped_model` when allowed, else `gpt-5.6-sol`, else the first model. `OPENCLAW_MODEL_MAPPING_JSON` may only
rename models the group allows. Cells get the mapping on create, provision and Repair.

The background pass (`SyncModelMappings`, every two minutes) is opt-in (`OPENCLAW_MODEL_SYNC_ENABLED=1`) because the
fleet worker's configure patch replaces the cell's default model, fallbacks and per-model settings and re-sends the
gateway section. When enabled, it writes a live cell only when what it should hold changed since it was last written:
a group model or default edit, or a replaced billing key. What each cell was given is kept in the `settings` row
`openclaw_cell_model_sync`, so a restart or deploy writes no cell. The first pass ever (row not yet seeded) records the
current mapping for every cell without writing any. The pass reads the group and never creates or repairs it.

The tenant fleet shows those models, whether each cell holds them (with the pass off, a cell not written since the
group changed is shown as needing Repair), every service per workspace, and counts only finished set-ups in workspaces
not queued for deletion. The customer Connections page counts connected services out of the services it offers,
matching its service cards, and names unfinished set-ups. No migration.

**Fork-owned files:** `backend/internal/service/openclaw_models.go`, `openclaw_models_test.go`.

**Hook points in shared files:**

| File | Hook |
| --- | --- |
| `backend/internal/service/openclaw.go` | Create, provision and Repair build the mapping with `s.workspaceModelMapping` and record it; `ListAdminInstances` reads the key's group and the connection rows. |
| `backend/internal/service/wire.go` | `ProvideOpenClawService` calls `StartModelSyncWorker`. |
| `frontend/src/views/admin/OpenClawFleetView.vue`, `frontend/src/api/openclaw.ts` | Services column, finished-set-up count, model source and sync state. |
| `frontend/src/views/user/ConnectionsView.vue`, zh/en `connections.ts` | `serviceSummary` drives the Connected services card. |

**Invariants checked by `fork-guard`:** the mapping is built from the group, the sync worker is started, sync state is
persisted, both pages carry their counts, and the tests in `openclaw_models_test.go`,
`OpenClawFleetView.usage.spec.ts` and `ConnectionsView.status.spec.ts` pass.

---

## Production is built from `main` only

Every production deploy must be built from a commit on `main`, with this guard green. Never deploy a locally built
tree: anything that exists only outside `main` is lost the next time someone deploys from `main`. If an urgent fix
must ship from a local tree, open the PR for it before (or right after) the deploy.

## Adding a new fork customization

1. Put the logic in fork-owned files and keep hooks in upstream files as small as possible.
2. Name new migrations `9NN_fork_<name>.sql` (see UPSTREAM_SYNC.md); never renumber a migration already applied in
   production.
3. Add an entry here, add its invariants and guard test files to `tools/fork-guard.sh`, and make sure CI is green.
