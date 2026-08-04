// MailList row actions: the delete button must be present by default and
// only disappear when a list explicitly opts out.
//
// Regression guard: the flag was first written as `deletable?: boolean`
// defaulting to "shown", but Vue casts an absent Boolean-typed prop to
// `false`, so every list that did not pass it silently lost its delete
// button. The prop is now an opt-out (`noDelete`) and this test pins it.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import MailList from '../MailList.vue'

vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))

const fetchMock = vi.fn()
vi.stubGlobal('fetch', fetchMock)

function res(status: number, body: unknown) {
  return { ok: status >= 200 && status < 300, status, text: async () => JSON.stringify(body) }
}

const listBody = {
  items: [{ server_ip_id: 1, ip_address: '10.0.0.1', ip_type: 'IPv4' }],
  total: 1,
}

const baseProps = {
  apiBase: '/api/server_ip',
  idField: 'server_ip_id',
  formBase: '/system/server-ips',
  columns: [
    { key: 'ip_type', label: 'serverip.col.type' },
    { key: 'ip_address', label: 'serverip.col.ip_address' },
  ],
  titleKey: 'serverip.list_title',
  addKey: 'serverip.add_ip',
}

beforeEach(() => {
  setActivePinia(createPinia())
  fetchMock.mockReset()
})

describe('MailList row actions', () => {
  it('renders the delete button when noDelete is not passed', async () => {
    fetchMock.mockResolvedValue(res(200, listBody))
    const wrapper = mount(MailList, { props: baseProps })
    await flushPromises()

    expect(wrapper.find('[data-test="delete"]').exists()).toBe(true)
  })

  it('hides the delete button when the list opts out', async () => {
    fetchMock.mockResolvedValue(res(200, listBody))
    const wrapper = mount(MailList, { props: { ...baseProps, noDelete: true } })
    await flushPromises()

    expect(wrapper.find('[data-test="delete"]').exists()).toBe(false)
  })

  it('hides the Add button only when addKey is empty', async () => {
    fetchMock.mockResolvedValue(res(200, listBody))
    const withAdd = mount(MailList, { props: baseProps })
    await flushPromises()
    expect(withAdd.find('[data-test="mail-add"]').exists()).toBe(true)

    fetchMock.mockResolvedValue(res(200, listBody))
    const without = mount(MailList, { props: { ...baseProps, addKey: '' } })
    await flushPromises()
    expect(without.find('[data-test="mail-add"]').exists()).toBe(false)
  })
})
