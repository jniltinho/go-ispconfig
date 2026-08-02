// DNS zone list view: rows from the DNS API, datalog badges, wizard/manual
// entry points. fetch and the router are mocked.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ZoneList from '../ZoneList.vue'

const push = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push }),
}))

const fetchMock = vi.fn()
vi.stubGlobal('fetch', fetchMock)

function res(status: number, body: unknown = '') {
  const text = typeof body === 'string' ? body : JSON.stringify(body)
  return { ok: status >= 200 && status < 300, status, text: async () => text }
}

const page = {
  items: [
    { id: 1, active: 'Y', server_id: 1, origin: 'example.com.', ns: 'ns1.example.net', mbox: 'admin.example.com.' },
    { id: 2, active: 'Y', server_id: 1, origin: 'pending.com.', ns: 'ns1.example.net', mbox: 'a.b.', _datalog_state: 'pending' },
    { id: 3, active: 'N', server_id: 1, origin: 'broken.com.', ns: 'ns1.example.net', mbox: 'a.b.', _datalog_state: 'error', _datalog_error: 'named-checkzone failed' },
  ],
  total: 3,
  page: 1,
  limit: 25,
}

beforeEach(() => {
  setActivePinia(createPinia())
  fetchMock.mockReset()
  push.mockReset()
})

describe('ZoneList', () => {
  it('renders zones with datalog badges', async () => {
    fetchMock.mockResolvedValueOnce(res(200, page))
    const wrapper = mount(ZoneList)
    await flushPromises()

    expect(fetchMock.mock.calls[0][0]).toBe('/api/dns/zones?page=1&limit=25')
    expect(wrapper.text()).toContain('example.com.')
    expect(wrapper.find('[data-test="state-pending"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="state-error"]').attributes('title')).toBe(
      'named-checkzone failed',
    )
  })

  it('navigates to the wizard and the manual form', async () => {
    fetchMock.mockResolvedValueOnce(res(200, page))
    const wrapper = mount(ZoneList)
    await flushPromises()

    await wrapper.find('[data-test="add-zone-wizard"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/dns/wizard')
    await wrapper.find('[data-test="add-zone-manual"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/dns/zones/new')
  })

  it('opens a zone on row click', async () => {
    fetchMock.mockResolvedValueOnce(res(200, page))
    const wrapper = mount(ZoneList)
    await flushPromises()

    await wrapper.findAll('tbody tr')[0].trigger('click')
    expect(push).toHaveBeenCalledWith('/dns/zones/1')
  })
})
