// Record editor grid: metadata-driven dialog with per-type fields and
// client-side validation, grid refresh after save. fetch is mocked.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import RecordGrid from '../RecordGrid.vue'

const fetchMock = vi.fn()
vi.stubGlobal('fetch', fetchMock)

function res(status: number, body: unknown = '') {
  const text = typeof body === 'string' ? body : JSON.stringify(body)
  return { ok: status >= 200 && status < 300, status, text: async () => text }
}

const types = [
  {
    type: 'A', stored_type: 'A', name_regex: '^[a-zA-Z0-9.\\-*]{0,64}$', name_required: false,
    data_kind: 'ipv4', data_label: 'data_a_txt', aux_used: false, aux_default: 0, ttl_default: 3600,
  },
  {
    type: 'MX', stored_type: 'MX', name_regex: '^[a-zA-Z0-9.\\-*]{0,255}$', name_required: false,
    data_kind: 'text', data_regex: '^[a-zA-Z0-9.\\-]{1,255}$', data_label: 'data_mx_txt',
    aux_used: true, aux_label: 'priority_txt', aux_default: 10, ttl_default: 3600,
  },
]

const zone = { id: 5, serial: 2026080102 }
const records = [
  { id: 11, type: 'A', name: 'www', data: '10.0.0.1', aux: 0, ttl: 3600, active: 'Y' },
]

// Queue the standard mount responses: record-types, zone, records.
function queueLoad() {
  fetchMock.mockImplementation((url: string) => {
    if (url === '/api/dns/record-types') return Promise.resolve(res(200, types))
    if (url === '/api/dns/zones/5') return Promise.resolve(res(200, zone))
    if (url === '/api/dns/zones/5/records') return Promise.resolve(res(200, records))
    return Promise.resolve(res(201, { id: 12 }))
  })
}

beforeEach(() => {
  // The delete flow goes through the themed confirm dialog, which is a
  // Pinia store.
  setActivePinia(createPinia())
  fetchMock.mockReset()
})

describe('RecordGrid', () => {
  it('renders the records and the zone serial', async () => {
    queueLoad()
    const wrapper = mount(RecordGrid, { props: { zoneId: '5' } })
    await flushPromises()

    expect(wrapper.text()).toContain('10.0.0.1')
    expect(wrapper.find('[data-test="zone-serial"]').text()).toContain('2026080102')
  })

  it('shows the priority input only for aux types', async () => {
    queueLoad()
    const wrapper = mount(RecordGrid, { props: { zoneId: '5' } })
    await flushPromises()

    await wrapper.find('[data-test="add-record"]').trigger('click')
    expect(wrapper.find('#rr-aux').exists()).toBe(false)

    await wrapper.find('#rr-type').setValue('MX')
    expect(wrapper.find('#rr-aux').exists()).toBe(true)
    expect((wrapper.find('#rr-aux').element as HTMLInputElement).value).toBe('10')
  })

  it('validates client-side before posting', async () => {
    queueLoad()
    const wrapper = mount(RecordGrid, { props: { zoneId: '5' } })
    await flushPromises()

    await wrapper.find('[data-test="add-record"]').trigger('click')
    await wrapper.find('#rr-data').setValue('not-an-ip')
    const calls = fetchMock.mock.calls.length
    await wrapper.find('[data-test="record-save"]').trigger('submit')
    await flushPromises()

    expect(fetchMock.mock.calls.length).toBe(calls)
    expect(wrapper.find('[data-test="record-dialog"]').text()).toContain(
      'The IP address is invalid.',
    )
  })

  it('posts a valid record and refreshes', async () => {
    queueLoad()
    const wrapper = mount(RecordGrid, { props: { zoneId: '5' } })
    await flushPromises()

    await wrapper.find('[data-test="add-record"]').trigger('click')
    await wrapper.find('#rr-name').setValue('api')
    await wrapper.find('#rr-data').setValue('10.0.0.2')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const post = fetchMock.mock.calls.find(
      (c) => c[0] === '/api/dns/zones/5/records' && c[1]?.method === 'POST',
    )
    expect(post).toBeTruthy()
    expect(JSON.parse(post![1].body)).toMatchObject({ type: 'A', name: 'api', data: '10.0.0.2' })
    expect(wrapper.emitted('changed')).toBeTruthy()
  })
})
