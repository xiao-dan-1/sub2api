import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('../client', () => ({
  apiClient: {
    get,
    post,
  },
}))

import {
  getPublicVersion,
  getRollbackVersions,
  performUpdate,
  rollback,
  type RollbackVersionInfo
} from '@/api/admin/system'

describe('admin system rollback API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('getRollbackVersions fetches the rollback version list', async () => {
    const versions: RollbackVersionInfo[] = [
      {
        version: '0.1.146',
        published_at: '2026-07-07T00:00:00Z',
        html_url: 'https://github.com/Wei-Shaw/sub2api/releases/tag/v0.1.146'
      }
    ]
    get.mockResolvedValue({ data: { versions } })

    const result = await getRollbackVersions()

    expect(get).toHaveBeenCalledWith('/admin/system/rollback-versions')
    expect(result.versions).toEqual(versions)
  })

  it('rollback posts the target version in the request body', async () => {
    post.mockResolvedValue({ data: { message: 'ok', need_restart: true } })

    const result = await rollback('0.1.146')

    expect(post).toHaveBeenCalledWith('/admin/system/rollback', { version: '0.1.146' })
    expect(result.need_restart).toBe(true)
  })

  it('rollback without a version posts no body (legacy backup rollback)', async () => {
    post.mockResolvedValue({ data: { message: 'ok', need_restart: true } })

    await rollback()

    expect(post).toHaveBeenCalledWith('/admin/system/rollback', undefined)
  })

  it('allows the custom container update request to outlive the default API timeout', async () => {
    post.mockResolvedValue({ data: { message: 'accepted', need_restart: false } })

    await performUpdate()

    expect(post).toHaveBeenCalledWith('/admin/system/update', undefined, { timeout: 660_000 })
  })

  it('polls the public version endpoint with a bounded request timeout', async () => {
    get.mockResolvedValue({ data: { version: '0.1.156-xd.5' } })

    const result = await getPublicVersion()

    expect(get).toHaveBeenCalledWith('/settings/public', { timeout: 5_000 })
    expect(result.version).toBe('0.1.156-xd.5')
  })
})
