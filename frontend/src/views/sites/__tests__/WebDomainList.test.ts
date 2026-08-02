// Websites list view: rows from the sites API, datalog state indicators,
// Add button and row-click navigation. fetch and the router are mocked.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import WebDomainList from '../WebDomainList.vue'

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
    { domain_id: 1, active: 'y', server_id: 1, domain: 'example.com', type: 'vhost' },
    {
      domain_id: 2,
      active: 'y',
      server_id: 1,
      domain: 'pending.com',
      type: 'vhost',
      _datalog_state: 'pending',
    },
    {
      domain_id: 3,
      active: 'n',
      server_id: 1,
      domain: 'broken.com',
      type: 'vhostsubdomain',
      _datalog_state: 'error',
      _datalog_error: 'nginx: -t failed',
    },
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

describe('WebDomainList', () => {
  it('renders rows from /api/sites/web-domains with state indicators', async () => {
    fetchMock.mockResolvedValueOnce(res(200, page))
    const wrapper = mount(WebDomainList)
    await flushPromises()

    expect(fetchMock.mock.calls[0][0]).toBe('/api/sites/web-domains?page=1&limit=25')
    expect(wrapper.text()).toContain('example.com')
    expect(wrapper.text()).toContain('Subdomain') // type label translated

    expect(wrapper.findAll('[data-test="state-pending"]')).toHaveLength(1)
    const error = wrapper.find('[data-test="state-error"]')
    expect(error.exists()).toBe(true)
    expect(error.attributes('title')).toBe('nginx: -t failed')
  })

  it('navigates to the create form from the Add button', async () => {
    fetchMock.mockResolvedValueOnce(res(200, { items: [], total: 0, page: 1, limit: 25 }))
    const wrapper = mount(WebDomainList)
    await flushPromises()

    await wrapper.find('button').trigger('click')
    expect(push).toHaveBeenCalledWith('/sites/domains/new')
  })

  it('opens the edit form on row click', async () => {
    fetchMock.mockResolvedValueOnce(res(200, page))
    const wrapper = mount(WebDomainList)
    await flushPromises()

    await wrapper.find('tbody tr').trigger('click')
    expect(push).toHaveBeenCalledWith('/sites/domains/1')
  })

  it('deletes a row after confirmation and reloads', async () => {
    fetchMock.mockResolvedValue(res(200, page))
    vi.stubGlobal('confirm', vi.fn().mockReturnValue(true))
    const wrapper = mount(WebDomainList)
    await flushPromises()

    await wrapper.find('[data-test="delete"]').trigger('click')
    await flushPromises()

    const del = fetchMock.mock.calls.find(([, init]) => init?.method === 'DELETE')
    expect(del?.[0]).toBe('/api/sites/web-domains/1')
    expect(fetchMock.mock.calls.length).toBeGreaterThanOrEqual(3) // list, delete, reload
  })

  it('requests a filtered first page when a column filter is applied', async () => {
    fetchMock.mockResolvedValue(res(200, page))
    const wrapper = mount(WebDomainList)
    await flushPromises()

    const filterInputs = wrapper.findAll('thead input')
    await filterInputs[2].setValue('exa') // domain column
    await filterInputs[2].trigger('keyup.enter')
    await flushPromises()

    expect(fetchMock.mock.calls[1][0]).toBe('/api/sites/web-domains?page=1&limit=25&domain=exa')
  })
})
