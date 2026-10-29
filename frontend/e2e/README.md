# Theme acceptance

Run `pnpm exec playwright install chromium`, then `pnpm test:ui` from `frontend`.
For installed Chrome on macOS, use `PORTAL_CHROME_CHANNEL=chrome pnpm test:ui`.

The suite renders component routes discovered in the router in light and dark
themes at 390px and 1440px. It also checks shared controls at 320, 390, 768, 1024,
and 1440px, theme persistence/system preference, pairing states, error messages,
and the mobile navigation backdrop. Parameterised legal/custom pages use example
identifiers. OAuth redirects and third-party payment/widget content are outside
the fixture audit; internal payment error screens are included.

API responses and users are synthetic. Browser API requests are intercepted,
including writes; no real payment, message, schedule, or account is changed.
`controls.html` mounts the real Vue components on Vite and is excluded from the
production build. Phone checks use Chromium emulation, not a physical phone.

Acceptance requires no unexpected page overflow, rendering errors, or axe text
contrast violations. The control checks also exercise focus, selected options,
switches, dates, tooltips, and dialogs. Review the screenshots before release;
passing automated checks alone does not establish visual correctness. CI keeps
the HTML report, screenshots, JSON results, and failure traces for seven days.

After deployment, confirm the deployed SHA and repeat the customer-facing smoke
checks against `PORTAL_BASE_URL=https://your-portal` with API fixtures. Then check
live login and authenticated pages using read-only access. Keep live account
screenshots local; never upload customer data with the fixture evidence.
