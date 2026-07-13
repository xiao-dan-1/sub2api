import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import RechargeCenterView from '@/views/user/RechargeCenterView.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const STORE_URL = 'https://catfk.com/shop/BD9COW6C'

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
