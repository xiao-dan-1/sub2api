# Recharge Center Design

## Goal

Add a dedicated recharge center to the authenticated Sub2API interface. The page keeps the existing Sub2API sidebar and header while embedding the live store at `https://pay.ldxp.cn/shop/FLTH3TZ2` in the main content area.

## User Experience

- Add a sidebar item named `充值中心` (`Recharge Center` in English).
- Place it immediately after `个人资料` in both the regular user navigation and the administrator's personal-account navigation.
- Use the existing storefront/recharge icon already present in `AppSidebar.vue`.
- Navigate to `/recharge-center` without leaving Sub2API.
- Fill the available viewport with the embedded store, matching the supplied reference: normal Sub2API chrome outside and the third-party store inside a bordered, rounded frame.
- Keep an always-visible `Open in new tab` action as a fallback for browser cookie restrictions or future iframe restrictions.
- Preserve the existing responsive sidebar behavior. On small screens, the frame uses the full content width and a practical minimum height.

## Architecture

### Route

Register `/recharge-center` as an authenticated, non-admin-only route. Route metadata provides the localized page title. It does not use the native payment feature flag because this page is an independent external-store integration.

### Navigation

Append the new item directly after `/profile` in `buildSelfNavItems()`. That shared navigation builder automatically exposes it to regular users and to administrators under `My Account`.

### View

Create `RechargeCenterView.vue` using `AppLayout`, the existing button and icon components, and project design tokens. The view owns:

- the fixed HTTPS store URL;
- a responsive iframe container;
- the iframe title and payment/clipboard permissions required by the live shop;
- an external-link fallback using `target="_blank"` and `rel="noopener noreferrer"`.

The iframe URL remains exactly the configured store URL. Sub2API user IDs, access tokens, or other authentication data must not be appended or passed to the third-party site.

## Error Handling

Browsers do not reliably expose cross-origin iframe policy failures to JavaScript. The page therefore always shows the new-window action instead of attempting an unreliable custom error detector. The target currently responds successfully and does not return `X-Frame-Options` or a restrictive `frame-ancestors` policy.

## Localization

Add `nav.rechargeCenter` in the existing Chinese and English locale files. Use this key for both the menu label and route title.

## Compatibility And Scope

- Keep the native `/purchase` payment/subscription flow unchanged.
- Do not change the backend, database schema, Docker services, or Docker image.
- Do not add a new runtime dependency.
- Do not forward Sub2API authentication details to the external store.

## Verification

- Unit test that the sidebar renders the recharge entry after the profile entry and links to `/recharge-center`.
- Unit test that the new view embeds the exact HTTPS URL and provides a secure new-window fallback.
- Run the focused Vitest tests, frontend typecheck, and production build.
- Verify the authenticated page in the browser at desktop and mobile viewport sizes, including iframe rendering, sidebar placement, and the new-window fallback.
