import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import VersionBadge from '@/components/common/VersionBadge.vue'

const systemAPIMocks = vi.hoisted(() => ({
  performUpdate: vi.fn(),
  restartService: vi.fn(),
  getRollbackVersions: vi.fn(),
  rollback: vi.fn()
}))

const customImageAPIMocks = vi.hoisted(() => ({
  checkCustomImageUpdate: vi.fn(),
  triggerCustomImageUpdate: vi.fn(),
  waitForCustomImageVersion: vi.fn(),
  isExpectedContainerReplacementError: vi.fn()
}))

const storeMocks = vi.hoisted(() => ({
  auth: { isAdmin: true },
  app: {
    versionLoading: false,
    currentVersion: '0.1.162-xd.4',
    latestVersion: '0.1.162',
    hasUpdate: false,
    releaseInfo: {
      name: 'v0.1.162',
      body: 'notes',
      published_at: '2026-07-20T00:00:00Z',
      html_url: 'https://github.com/Wei-Shaw/sub2api/releases/tag/v0.1.162'
    },
    buildType: 'release',
    fetchVersion: vi.fn(),
    clearVersionCache: vi.fn()
  }
}))

vi.mock('@/api/admin/system', () => ({
  performUpdate: systemAPIMocks.performUpdate,
  restartService: systemAPIMocks.restartService,
  getRollbackVersions: systemAPIMocks.getRollbackVersions,
  rollback: systemAPIMocks.rollback
}))

vi.mock('@/api/admin/custom-image-update', () => ({
  checkCustomImageUpdate: customImageAPIMocks.checkCustomImageUpdate,
  triggerCustomImageUpdate: customImageAPIMocks.triggerCustomImageUpdate,
  waitForCustomImageVersion: customImageAPIMocks.waitForCustomImageVersion,
  isExpectedContainerReplacementError: customImageAPIMocks.isExpectedContainerReplacementError,
  CustomImageRestartTimeoutError: class CustomImageRestartTimeoutError extends Error {}
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => storeMocks.auth,
  useAppStore: () => storeMocks.app
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copied: { value: false },
    copyToClipboard: vi.fn()
  })
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string>) =>
        params?.version ? `${key}:${params.version}` : key
    })
  }
})

const readyInfo = {
  current_version: '0.1.162-xd.4',
  target_version: '0.1.162-xd.5',
  image: 'ghcr.io/xiao-dan-1/sub2api',
  target_image: 'ghcr.io/xiao-dan-1/sub2api:0.1.162-xd.5',
  target_digest: 'sha256:ready',
  release_url: 'https://github.com/xiao-dan-1/sub2api/releases/tag/v0.1.162-xd.5',
  has_update: true,
  target_ready: true,
  latest_alias_ready: true,
  ready: true,
  warning: ''
}

function mountBadge() {
  return mount(VersionBadge, {
    props: { version: '0.1.162-xd.4' },
    global: {
      stubs: {
        Icon: { template: '<span class="icon-stub" />' }
      }
    }
  })
}

async function mountAndOpenBadge() {
  const wrapper = mountBadge()
  await flushPromises()
  await wrapper.find('button').trigger('click')
  await nextTick()
  return wrapper
}

describe('VersionBadge custom container update', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    Object.assign(storeMocks.app, {
      versionLoading: false,
      currentVersion: '0.1.162-xd.4',
      latestVersion: '0.1.162',
      hasUpdate: false,
      buildType: 'release'
    })
    customImageAPIMocks.checkCustomImageUpdate.mockResolvedValue({ ...readyInfo })
    customImageAPIMocks.triggerCustomImageUpdate.mockResolvedValue({
      message: 'scheduled',
      operation_id: 'sysop-123',
      target_version: '0.1.162-xd.5',
      target_image: 'ghcr.io/xiao-dan-1/sub2api:0.1.162-xd.5',
      target_digest: 'sha256:ready',
      automatic_restart: true
    })
    customImageAPIMocks.waitForCustomImageVersion.mockImplementation(() => new Promise(() => undefined))
    customImageAPIMocks.isExpectedContainerReplacementError.mockReturnValue(false)
  })

  it('detects an xd build from its version and shows the isolated ready target', async () => {
    const wrapper = await mountAndOpenBadge()

    expect(customImageAPIMocks.checkCustomImageUpdate).toHaveBeenCalledTimes(1)
    const ready = wrapper.get('[data-testid="custom-update-ready"]')
    expect(ready.text()).toContain('0.1.162-xd.5')
    expect(wrapper.get('[data-testid="custom-update-button"]').exists()).toBe(true)
    expect(
      wrapper.get('a[href="https://github.com/Wei-Shaw/sub2api/releases/tag/v0.1.162"]').exists()
    ).toBe(true)

    wrapper.unmount()
  })

  it('shows registry readiness warnings without offering an unsafe update', async () => {
    customImageAPIMocks.checkCustomImageUpdate.mockResolvedValue({
      ...readyInfo,
      ready: false,
      latest_alias_ready: false,
      warning: 'custom container image latest tag does not yet match 0.1.162-xd.5'
    })

    const wrapper = await mountAndOpenBadge()

    const waiting = wrapper.get('[data-testid="custom-update-waiting"]')
    expect(waiting.text()).toContain('latest')
    expect(wrapper.find('[data-testid="custom-update-button"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('uses the custom endpoint and delegates restart polling to the custom module', async () => {
    const wrapper = await mountAndOpenBadge()

    await wrapper.get('[data-testid="custom-update-button"]').trigger('click')
    await flushPromises()

    expect(customImageAPIMocks.triggerCustomImageUpdate).toHaveBeenCalledTimes(1)
    expect(systemAPIMocks.performUpdate).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="custom-update-reconnecting"]').text()).toContain(
      '0.1.162-xd.5'
    )
    expect(customImageAPIMocks.waitForCustomImageVersion).toHaveBeenCalledWith(
      '0.1.162-xd.5',
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )

    wrapper.unmount()
  })

  it('starts polling after an expected connection loss during replacement', async () => {
    customImageAPIMocks.triggerCustomImageUpdate.mockRejectedValue({
      status: 0,
      message: 'Network error'
    })
    customImageAPIMocks.isExpectedContainerReplacementError.mockReturnValue(true)
    const wrapper = await mountAndOpenBadge()

    await wrapper.get('[data-testid="custom-update-button"]').trigger('click')
    await flushPromises()

    expect(customImageAPIMocks.waitForCustomImageVersion).toHaveBeenCalledWith(
      '0.1.162-xd.5',
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(wrapper.get('[data-testid="custom-update-reconnecting"]').exists()).toBe(true)

    wrapper.unmount()
  })

  it('aborts module-owned polling when the component unmounts', async () => {
    const wrapper = await mountAndOpenBadge()
    await wrapper.get('[data-testid="custom-update-button"]').trigger('click')
    await flushPromises()
    const options = customImageAPIMocks.waitForCustomImageVersion.mock.calls[0][1]

    wrapper.unmount()

    expect(options.signal.aborted).toBe(true)
  })
})
