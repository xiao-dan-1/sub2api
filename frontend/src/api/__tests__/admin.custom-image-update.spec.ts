import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn()
}))

vi.mock('../client', () => ({
  apiClient: {
    get,
    post
  }
}))

import {
  CustomImageRestartTimeoutError,
  checkCustomImageUpdate,
  getPublicVersion,
  isExpectedContainerReplacementError,
  triggerCustomImageUpdate,
  waitForCustomImageVersion
} from '@/api/admin/custom-image-update'

describe('admin custom image update API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('checks custom image readiness on the isolated endpoint', async () => {
    get.mockResolvedValue({
      data: {
        current_version: '0.1.162-xd.4',
        target_version: '0.1.162-xd.5',
        image: 'ghcr.io/xiao-dan-1/sub2api',
        target_image: 'ghcr.io/xiao-dan-1/sub2api:0.1.162-xd.5',
        target_digest: 'sha256:ready',
        has_update: true,
        target_ready: true,
        latest_alias_ready: true,
        ready: true
      }
    })

    const result = await checkCustomImageUpdate()

    expect(get).toHaveBeenCalledWith('/admin/system/custom-image/check')
    expect(result.target_version).toBe('0.1.162-xd.5')
    expect(result.target_digest).toBe('sha256:ready')
    expect(result.ready).toBe(true)
  })

  it('triggers Watchtower without accepting a client-selected image and allows eleven minutes', async () => {
    post.mockResolvedValue({
      data: {
        message: 'scheduled',
        operation_id: 'sysop-123',
        target_version: '0.1.162-xd.5',
        target_image: 'ghcr.io/xiao-dan-1/sub2api:0.1.162-xd.5',
        target_digest: 'sha256:ready',
        automatic_restart: true
      }
    })

    const result = await triggerCustomImageUpdate()

    expect(post).toHaveBeenCalledWith('/admin/system/custom-image/update', undefined, {
      timeout: 660_000
    })
    expect(result.automatic_restart).toBe(true)
    expect(result.target_version).toBe('0.1.162-xd.5')
  })

  it('reads the public running version with a bounded request timeout', async () => {
    get.mockResolvedValue({ data: { version: '0.1.162-xd.5' } })

    const result = await getPublicVersion()

    expect(get).toHaveBeenCalledWith('/settings/public', { timeout: 5_000 })
    expect(result.version).toBe('0.1.162-xd.5')
  })

  it('waits 1.5 seconds then polls every 2 seconds until the target version appears', async () => {
    vi.useFakeTimers()
    get
      .mockResolvedValueOnce({ data: { version: '0.1.162-xd.4' } })
      .mockResolvedValueOnce({ data: { version: '0.1.162-xd.5' } })

    const waiting = waitForCustomImageVersion('0.1.162-xd.5')

    expect(get).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1_500)
    expect(get).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(2_000)
    await expect(waiting).resolves.toEqual({ version: '0.1.162-xd.5' })
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('tolerates temporary connection failures while the container is replaced', async () => {
    vi.useFakeTimers()
    get
      .mockRejectedValueOnce({ status: 0, message: 'Network error' })
      .mockResolvedValueOnce({ data: { version: '0.1.162-xd.5' } })

    const waiting = waitForCustomImageVersion('0.1.162-xd.5')

    await vi.advanceTimersByTimeAsync(1_500)
    await vi.advanceTimersByTimeAsync(2_000)
    await expect(waiting).resolves.toEqual({ version: '0.1.162-xd.5' })
  })

  it('stops after the restart deadline with a typed actionable timeout', async () => {
    vi.useFakeTimers()
    get.mockRejectedValue({ status: 0, message: 'offline' })

    const waiting = waitForCustomImageVersion('0.1.162-xd.5', {
      initialDelayMs: 10,
      pollIntervalMs: 20,
      timeoutMs: 55
    })

    const rejection = expect(waiting).rejects.toBeInstanceOf(CustomImageRestartTimeoutError)
    await vi.advanceTimersByTimeAsync(100)
    await rejection
  })

  it('can abort polling when the owning component unmounts', async () => {
    vi.useFakeTimers()
    get.mockRejectedValue({ status: 0, message: 'offline' })
    const controller = new AbortController()

    const waiting = waitForCustomImageVersion('0.1.162-xd.5', {
      signal: controller.signal
    })
    controller.abort()
    await vi.runAllTimersAsync()

    await expect(waiting).resolves.toBeNull()
    expect(get).not.toHaveBeenCalled()
  })

  it('distinguishes expected replacement disconnects from API responses', () => {
    expect(isExpectedContainerReplacementError({ status: 0, message: 'Network error' })).toBe(true)
    expect(
      isExpectedContainerReplacementError({ code: 'ERR_NETWORK', message: 'Network error' })
    ).toBe(true)
    expect(isExpectedContainerReplacementError(new Error('application bug'))).toBe(false)
    expect(isExpectedContainerReplacementError({ message: 'application bug' })).toBe(false)
    expect(isExpectedContainerReplacementError({ status: 409, code: 'CUSTOM_UPDATE_NOT_READY' })).toBe(
      false
    )
  })
})
