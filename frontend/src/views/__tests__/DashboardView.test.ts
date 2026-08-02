// Dashboard dashlets: one card per non-dashboard module with icon, title
// and a full-width RouterLink button.
import { describe, expect, it } from 'vitest'
import { mount, RouterLinkStub } from '@vue/test-utils'
import DashboardView from '../DashboardView.vue'
import { modules } from '../../modules'

describe('DashboardView', () => {
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
})
