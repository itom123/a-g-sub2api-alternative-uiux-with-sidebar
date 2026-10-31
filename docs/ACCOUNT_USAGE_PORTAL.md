# Usage and balance in the user sidebar

`/dashboard` is the signed-in account's **Usage & balance** overview. It replaces the need to install API Balance Portal just to see that account's consumption. The existing `/usage` page remains the full, paginated call history.

The overview shows current available balance, a separate frozen amount when applicable, today's actual charges, request and token totals, platform allowances, spending and token trends, model distribution and the ten newest calls in the selected date range. The default range is the last seven local calendar days and follows the current day until the user chooses a range.

## Account and money boundaries

All six reads use existing authenticated user endpoints: `/user/profile`, `/user/platform-quotas`, `/usage/dashboard/stats`, `/usage/dashboard/trend`, `/usage/dashboard/models` and `/usage`. There is no user ID input and no admin aggregation call. Existing backend subject/ownership checks apply. Provider credentials, company upstream balances and other members' consumption are outside this page's scope.

The ledger remains USD. The existing currency service supplies CNY presentation, and actual charges use `actual_cost`, not list-price `cost`. There is no invented balance percentage or recharge total. The optional balance meter compares available funds with the separate frozen funds; platform progress bars use real limits. Missing currency rates remain unavailable rather than being treated as zero.

## Refresh and failures

Automatic refresh is enabled by default every 30 seconds. The account-specific browser preference can disable polling; response data is not saved as a new browser cache. Hidden pages pause and refresh on return, and navigation removes the timer and visibility listener. An unfinished request blocks further automatic ticks.

The view commits all six successful reads together. If any read fails, it keeps the last complete snapshot, shows a stale-data warning with its original date range and leaves its update time unchanged. An initial failure exposes a retry instead of showing fabricated zero usage. Older date-range responses cannot overwrite newer results. Changing the signed-in account clears the old snapshot before loading the new one.

## Design and verification

The page retains the portal's paper/ink palette (`#ffffff`, `#000000`), existing AG typography and semantic availability/warning colors. Four totals form one desktop row and two mobile columns. Spending is the principal diagram; model distribution, token detail and quota meters use the existing chart system. A single observed point stays visible without inventing a continuous trend.

`DashboardView.spec.ts` exercises the actual view's lifecycle, stale data, date races and account switching. `account-usage.spec.ts` exercises ordinary-user navigation, currency and frozen balances, the matching header balance, manual failure recovery, signed-out redirects, both themes and phone/desktop layouts. Backend usage-handler tests verify subject scoping and rejection of cross-user API keys. The tests call no real provider APIs.

## Release and rollback

Hosted CI runs the frontend checks and browser acceptance, then builds `backend/internal/web/dist` and uploads `sub2api-frontend` for 14 days. Use the artifact from the exact reviewed commit after all CI jobs pass and that commit reaches `main`. Deployment must preserve the site's existing runtime configuration and old lazy chunks, activate a new frontend release atomically and retain the previous frontend symlink target for rollback. This page needs no database migration or backend replacement.

The API Balance Portal repository records the [desktop-to-web data mapping](https://github.com/itom123/api-balance-portal/blob/main/docs/sub2api-account-usage-migration.md). Its full local archive and recovery instructions are recorded privately in the owner's Notion notes. No credentials or private archive are committed here.
