# Recharge Center Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an authenticated Recharge Center menu item after Profile that embeds `https://pay.ldxp.cn/shop/FLTH3TZ2` inside the existing Sub2API layout.

**Architecture:** A dedicated Vue view owns the fixed third-party URL and renders it in a viewport-filling iframe with a secure external-window fallback. The existing shared user navigation builder exposes the route to regular users and administrators, while Vue Router and i18n provide authentication and localized titles. No backend or Docker changes are required.

**Tech Stack:** Vue 3, TypeScript, Vue Router, vue-i18n, Tailwind CSS, Vitest, Vue Test Utils

---

## File Structure

- Create `frontend/src/views/user/RechargeCenterView.vue`: isolated iframe page and external fallback.
- Create `frontend/src/views/user/__tests__/RechargeCenterView.spec.ts`: iframe URL, permissions, title, and safe fallback coverage.
- Modify `frontend/src/components/layout/AppSidebar.vue`: add the shared navigation entry after Profile using the existing recharge icon.
- Modify `frontend/src/components/layout/__tests__/AppSidebar.spec.ts`: lock the menu label, path, icon, and ordering.
- Modify `frontend/src/router/index.ts`: register the authenticated `/recharge-center` route.
- Modify `frontend/src/i18n/locales/zh/common.ts`: Chinese menu and route title.
- Modify `frontend/src/i18n/locales/en/common.ts`: English menu and route title.

### Task 1: Navigation Contract

**Files:**
- Modify: `frontend/src/components/layout/__tests__/AppSidebar.spec.ts`
- Modify: `frontend/src/components/layout/AppSidebar.vue`
- Modify: `frontend/src/i18n/locales/zh/common.ts`
- Modify: `frontend/src/i18n/locales/en/common.ts`

- [ ] **Step 1: Write the failing sidebar test**

Append this suite to `AppSidebar.spec.ts`:

```ts
describe('AppSidebar recharge center navigation', () => {
  it('places Recharge Center immediately after Profile', () => {
    const profileEntry = "{ path: '/profile', label: t('nav.profile'), icon: UserIcon }"
    const rechargeEntry = "{ path: '/recharge-center', label: t('nav.rechargeCenter'), icon: RechargeSubscriptionIcon }"
    const profileIndex = componentSource.indexOf(profileEntry)
    const rechargeIndex = componentSource.indexOf(rechargeEntry)
    const nextEntryIndex = componentSource.indexOf('\n    { path:', profileIndex + profileEntry.length)

    expect(profileIndex).toBeGreaterThan(-1)
    expect(rechargeIndex).toBeGreaterThan(-1)
    expect(rechargeIndex).toBe(nextEntryIndex + 5)
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run from `frontend`:

```powershell
npm run test:run -- src/components/layout/__tests__/AppSidebar.spec.ts
```

Expected: FAIL because `/recharge-center` is not present.

- [ ] **Step 3: Add the menu item and locale labels**

Add immediately after the existing `/profile` item:

```ts
{ path: '/recharge-center', label: t('nav.rechargeCenter'), icon: RechargeSubscriptionIcon },
```

Add `rechargeCenter: '充值中心'` after `profile` in the Chinese `nav` object, and add `rechargeCenter: 'Recharge Center'` in the same position in English.

- [ ] **Step 4: Run the sidebar test to verify it passes**

```powershell
npm run test:run -- src/components/layout/__tests__/AppSidebar.spec.ts
```

Expected: all tests in the file PASS.

- [ ] **Step 5: Commit the navigation contract**

```powershell
git add frontend/src/components/layout/AppSidebar.vue frontend/src/components/layout/__tests__/AppSidebar.spec.ts frontend/src/i18n/locales/zh/common.ts frontend/src/i18n/locales/en/common.ts
git commit -m "feat: add recharge center navigation"
```

### Task 2: Embedded Recharge Page

**Files:**
- Create: `frontend/src/views/user/__tests__/RechargeCenterView.spec.ts`
- Create: `frontend/src/views/user/RechargeCenterView.vue`
- Modify: `frontend/src/router/index.ts`

- [ ] **Step 1: Write the failing page test**

Create `RechargeCenterView.spec.ts`:

```ts
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import RechargeCenterView from '@/views/user/RechargeCenterView.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const STORE_URL = 'https://pay.ldxp.cn/shop/FLTH3TZ2'

function mountView() {
  return mount(RechargeCenterView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        Icon: true
      }
    }
  })
}

describe('RechargeCenterView', () => {
  it('embeds the configured store without Sub2API credentials', () => {
    const wrapper = mountView()
    const frame = wrapper.get('iframe')

    expect(frame.attributes('src')).toBe(STORE_URL)
    expect(frame.attributes('src')).not.toContain('token=')
    expect(frame.attributes('src')).not.toContain('user_id=')
    expect(frame.attributes('title')).toBe('nav.rechargeCenter')
    expect(frame.attributes('allow')).toContain('payment')
  })

  it('provides a safe new-window fallback', () => {
    const wrapper = mountView()
    const fallback = wrapper.get('[data-testid="recharge-center-external-link"]')

    expect(fallback.attributes('href')).toBe(STORE_URL)
    expect(fallback.attributes('target')).toBe('_blank')
    expect(fallback.attributes('rel')).toBe('noopener noreferrer')
  })
})
```

- [ ] **Step 2: Run the page test to verify it fails**

```powershell
npm run test:run -- src/views/user/__tests__/RechargeCenterView.spec.ts
```

Expected: FAIL because `RechargeCenterView.vue` does not exist.

- [ ] **Step 3: Implement the dedicated view**

Create `RechargeCenterView.vue`:

```vue
<template>
  <AppLayout>
    <section class="recharge-center" data-testid="recharge-center">
      <div class="recharge-center-shell">
        <a
          :href="storeUrl"
          target="_blank"
          rel="noopener noreferrer"
          class="btn btn-secondary btn-sm recharge-center-external"
          data-testid="recharge-center-external-link"
        >
          <Icon name="externalLink" size="sm" :stroke-width="2" />
          <span class="hidden sm:inline">{{ t('purchase.openInNewTab') }}</span>
        </a>
        <iframe
          :src="storeUrl"
          :title="t('nav.rechargeCenter')"
          class="recharge-center-frame"
          allow="clipboard-write; payment"
        ></iframe>
      </div>
    </section>
  </AppLayout>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const storeUrl = 'https://pay.ldxp.cn/shop/FLTH3TZ2'
</script>

<style scoped>
.recharge-center {
  height: calc(100vh - 6rem);
  height: calc(100dvh - 6rem);
  min-height: 36rem;
}

.recharge-center-shell {
  @apply relative h-full w-full overflow-hidden rounded-lg border border-gray-200 bg-white shadow-sm;
  @apply dark:border-dark-700 dark:bg-dark-900;
}

.recharge-center-external {
  @apply absolute right-3 top-3 z-10 gap-1.5 shadow-sm;
  @apply backdrop-blur supports-[backdrop-filter]:bg-white/90;
  @apply dark:supports-[backdrop-filter]:bg-dark-800/90;
}

.recharge-center-frame {
  @apply block h-full w-full border-0 bg-white;
}

@media (min-width: 768px) {
  .recharge-center {
    height: calc(100vh - 7rem);
    height: calc(100dvh - 7rem);
  }
}

@media (min-width: 1024px) {
  .recharge-center {
    height: calc(100vh - 8rem);
    height: calc(100dvh - 8rem);
  }
}
</style>
```

- [ ] **Step 4: Register the authenticated route**

Add immediately after `/profile`:

```ts
{
  path: '/recharge-center',
  name: 'RechargeCenter',
  component: () => import('@/views/user/RechargeCenterView.vue'),
  meta: {
    requiresAuth: true,
    requiresAdmin: false,
    title: 'Recharge Center',
    titleKey: 'nav.rechargeCenter'
  }
},
```

- [ ] **Step 5: Run focused tests and commit**

```powershell
npm run test:run -- src/views/user/__tests__/RechargeCenterView.spec.ts src/components/layout/__tests__/AppSidebar.spec.ts
git add frontend/src/views/user/RechargeCenterView.vue frontend/src/views/user/__tests__/RechargeCenterView.spec.ts frontend/src/router/index.ts
git commit -m "feat: embed recharge center store"
```

Expected: both test files PASS before committing.

### Task 3: Full Verification And Visual QA

**Files:**
- Verify only; fix files from Tasks 1-2 if a check identifies a defect.

- [ ] **Step 1: Run static checks and production build**

```powershell
npm run typecheck
npm run lint:check
npm run build
```

Expected: all commands exit successfully.

- [ ] **Step 2: Verify services**

```powershell
curl.exe -sS http://127.0.0.1:8080/health
curl.exe -sS -o NUL -w "%{http_code}" http://127.0.0.1:3000
```

Expected: backend returns `{"status":"ok"}` and frontend returns HTTP `200`.

- [ ] **Step 3: Verify the authenticated desktop experience**

Open `http://127.0.0.1:3000/recharge-center` and verify that `充值中心` is immediately below `个人资料`, the item is active, Sub2API chrome remains visible, the store renders, the fallback targets the exact URL, and no content overlaps.

- [ ] **Step 4: Verify the mobile experience**

At a mobile viewport, verify that the iframe fills the available content width, has a usable height, and the fallback remains inside the frame without hiding important controls.

- [ ] **Step 5: Review the final branch**

```powershell
git diff --check HEAD~2..HEAD
git status --short
git log -3 --oneline
```

Expected: no whitespace errors, a clean worktree, and focused commits at the branch tip.

