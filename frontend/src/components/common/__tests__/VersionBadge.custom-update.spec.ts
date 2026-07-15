import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import VersionBadge from '@/components/common/VersionBadge.vue'

const apiMocks = vi.hoisted(() => ({
  performUpdate: vi.fn(),
  checkUpdates: vi.fn(),
  getVersion: vi.fn(),
  getPublicVersion: vi.fn(),
  restartService: vi.fn(),
  getRollbackVersions: vi.fn(),
  rollback: vi.fn()
}))

const storeMocks = vi.hoisted(() => ({
  auth: { isAdmin: true },
  app: {
    versionLoading: false,
    currentVersion: '0.1.156-xd.4',
    latestVersion: '0.1.156',
    hasUpdate: true,
    releaseInfo: {
      name: 'v0.1.156',
      body: 'notes',
      published_at: '2026-07-15T00:00:00Z',
      html_url: 'https://github.com/Wei-Shaw/sub2api/releases/tag/v0.1.156'
    },
    buildType: 'custom',
    customVersion: '0.1.156-xd.5',
    customImage: 'ghcr.io/xiao-dan-1/sub2api',
    customReleaseUrl: 'https://github.com/xiao-dan-1/sub2api/releases/tag/v0.1.156-xd.5',
    customUpdateAvailable: true,
    customUpdateReady: true,
    customUpdateWarning: '',
    fetchVersion: vi.fn(),
    clearVersionCache: vi.fn()
  }
}))

vi.mock('@/api/admin/system', () => ({
  performUpdate: apiMocks.performUpdate,
  checkUpdates: apiMocks.checkUpdates,
  getVersion: apiMocks.getVersion,
  getPublicVersion: apiMocks.getPublicVersion,
  restartService: apiMocks.restartService,
  getRollbackVersions: apiMocks.getRollbackVersions,
  rollback: apiMocks.rollback
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

function mountBadge() {
  return mount(VersionBadge, {
    props: { version: '0.1.156-xd.4' },
    global: {
      stubs: {
        Icon: { template: '<span class="icon-stub" />' }
      }
    }
  })
}

async function openDropdown(wrapper: ReturnType<typeof mountBadge>) {
  await wrapper.find('button').trigger('click')
  await nextTick()
}

describe('VersionBadge custom container update', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    Object.assign(storeMocks.app, {
      versionLoading: false,
      currentVersion: '0.1.156-xd.4',
      latestVersion: '0.1.156',
      hasUpdate: true,
      buildType: 'custom',
      customVersion: '0.1.156-xd.5',
      customImage: 'ghcr.io/xiao-dan-1/sub2api',
      customUpdateAvailable: true,
      customUpdateReady: true,
      customUpdateWarning: ''
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('keeps the author release visible and shows the exact ready custom target', async () => {
    const wrapper = mountBadge()
    await openDropdown(wrapper)

    const ready = wrapper.get('[data-testid="custom-update-ready"]')
    expect(ready.text()).toContain('0.1.156-xd.5')
    expect(wrapper.get('[data-testid="custom-update-button"]').exists()).toBe(true)
    expect(
      wrapper.get('a[href="https://github.com/Wei-Shaw/sub2api/releases/tag/v0.1.156"]').exists()
    ).toBe(true)

    wrapper.unmount()
  })

  it('shows a waiting state without offering the upstream image', async () => {
    Object.assign(storeMocks.app, {
      latestVersion: '0.1.157',
      customVersion: '',
      customUpdateAvailable: false,
      customUpdateReady: false,
      customUpdateWarning: 'waiting for custom container image matching 0.1.157'
    })
    const wrapper = mountBadge()
    await openDropdown(wrapper)

    const waiting = wrapper.get('[data-testid="custom-update-waiting"]')
    expect(waiting.text()).toContain('0.1.157')
    expect(waiting.text()).toContain('version.customBuildPendingHint')
    expect(wrapper.find('[data-testid="custom-update-button"]').exists()).toBe(false)
    expect(
      wrapper.get('a[href="https://github.com/Wei-Shaw/sub2api/releases/tag/v0.1.156"]').exists()
    ).toBe(true)

    wrapper.unmount()
  })

  it('polls the public version endpoint while reconnecting after an automatic restart', async () => {
    vi.useFakeTimers()
    apiMocks.performUpdate.mockResolvedValue({
      message: 'scheduled',
      need_restart: false,
      automatic_restart: true,
      target_version: '0.1.156-xd.5',
      target_image: 'ghcr.io/xiao-dan-1/sub2api:latest'
    })
    apiMocks.getPublicVersion.mockImplementation(() => new Promise(() => undefined))
    const wrapper = mountBadge()
    await openDropdown(wrapper)

    await wrapper.get('[data-testid="custom-update-button"]').trigger('click')
    await flushPromises()

    expect(apiMocks.performUpdate).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid="custom-update-reconnecting"]').text()).toContain(
      '0.1.156-xd.5'
    )
    await vi.advanceTimersByTimeAsync(1500)
    expect(apiMocks.getPublicVersion).toHaveBeenCalledTimes(1)
    expect(apiMocks.getVersion).not.toHaveBeenCalled()
    expect(apiMocks.checkUpdates).not.toHaveBeenCalled()

    wrapper.unmount()
  })

  it('stops public version polling after the component unmounts', async () => {
    vi.useFakeTimers()
    apiMocks.performUpdate.mockResolvedValue({
      message: 'accepted',
      need_restart: false,
      automatic_restart: true,
      target_version: '0.1.156-xd.5',
      target_image: 'ghcr.io/xiao-dan-1/sub2api:latest'
    })
    apiMocks.getPublicVersion.mockRejectedValue(new Error('offline'))
    const wrapper = mountBadge()
    await openDropdown(wrapper)

    await wrapper.get('[data-testid="custom-update-button"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1500)
    expect(apiMocks.getPublicVersion).toHaveBeenCalledTimes(1)

    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(10_000)

    expect(apiMocks.getPublicVersion).toHaveBeenCalledTimes(1)
  })
})
