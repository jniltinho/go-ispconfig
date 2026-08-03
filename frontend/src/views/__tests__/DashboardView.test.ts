// Dashboard dashlets: one card per non-dashboard module with icon, title
// and a full-width RouterLink button.
import { beforeEach, describe, expect, it } from 'vitest'
import { mount, RouterLinkStub } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import DashboardView from '../DashboardView.vue'
import { modules } from '../../modules'

describe('DashboardView', () => {
  // The welcome heading reads the session username from the auth store.
  beforeEach(() => setActivePinia(createPinia()))

  it('renders a dashlet per module with a link into it', () => {
    const wrapper = mount(DashboardView, {
      global: { stubs: { RouterLink: RouterLinkStub } },
    })
    for (const mod of modules.filter((m) => m.id !== 'dashboard')) {
      const dashlet = wrapper.find(`[data-test="dashlet-${mod.id}"]`)
      expect(dashlet.exists()).toBe(true)
      expect(dashlet.find('svg').exists()).toBe(true)
      expect(dashlet.findComponent(RouterLinkStub).props('to')).toBe(mod.path)
    }
    expect(wrapper.find('[data-test="dashlet-dashboard"]').exists()).toBe(false)
  })

  // Every dashlet sits in a DashletCard whose title bar is a real heading,
  // so the red strip stays pure decoration (legacy .table-wrapper caption).
  it('wraps dashlets in cards with a heading title bar', () => {
    const wrapper = mount(DashboardView, {
      global: { stubs: { RouterLink: RouterLinkStub } },
    })
    const head = wrapper.find('section > h2')
    expect(head.exists()).toBe(true)
    expect(head.classes()).toContain('bg-brand')
  })
})
