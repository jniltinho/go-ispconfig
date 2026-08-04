// Server Config editor: tabs map onto INI sections, checkbox values round
// trip as y/n strings, and save PUTs only the sections the operator actually
// changed (the API merges section by section, so a blind PUT of every tab
// would rewrite untouched sections on every save). fetch is mocked.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ServerConfigView from '../ServerConfigView.vue'

const push = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push }),
  useRoute: () => ({ params: { id: '1' } }),
}))

const fetchMock = vi.fn()
vi.stubGlobal('fetch', fetchMock)

function res(status: number, body: unknown = '') {
  const text = typeof body === 'string' ? body : JSON.stringify(body)
  return { ok: status >= 200 && status < 300, status, text: async () => text }
}

const meta = {
  name: 'server_config',
  title: 'srvcfg.title',
  tabs: [
    {
      name: 'web',
      label: 'srvcfg.tab.web',
      fields: [
        {
          name: 'website_path',
          label: 'srvcfg.web.website_path',
          type: 'text',
          datatype: 'VARCHAR',
          formtype: 'TEXT',
        },
        {
          name: 'enable_sni',
          label: 'srvcfg.web.enable_sni',
          type: 'checkbox',
          datatype: 'VARCHAR',
          formtype: 'CHECKBOX',
          default: 'y',
          options: [
            { value: 'n', label: 'n' },
            { value: 'y', label: 'y' },
          ],
        },
      ],
    },
    {
      name: 'dns',
      label: 'srvcfg.tab.dns',
      fields: [
        {
          name: 'bind_user',
          label: 'srvcfg.dns.bind_user',
          type: 'text',
          datatype: 'VARCHAR',
          formtype: 'TEXT',
        },
      ],
    },
  ],
}

const config = {
  web: { website_path: '/var/www/$domain', enable_sni: 'y' },
  dns: { bind_user: 'root' },
}

function mountView() {
  fetchMock.mockResolvedValueOnce(res(200, meta))
  fetchMock.mockResolvedValueOnce(res(200, config))
  fetchMock.mockResolvedValueOnce(res(200, { server_name: 'debian.goisp.test' }))
  return mount(ServerConfigView)
}

beforeEach(() => {
  fetchMock.mockReset()
  push.mockReset()
})

describe('ServerConfigView', () => {
  it('renders one tab per INI section with the stored values', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(fetchMock.mock.calls[0][0]).toBe('/api/meta/forms/server_config')
    expect(fetchMock.mock.calls[1][0]).toBe('/api/servers/1/config')
    expect(wrapper.find('[data-test="server-name"]').text()).toContain('debian.goisp.test')
    expect(wrapper.findAll('button[role="tab"], [role="tab"]').length).toBeGreaterThanOrEqual(2)
    const path = wrapper.find('#field-website_path')
    expect((path.element as HTMLInputElement).value).toBe('/var/www/$domain')
    expect((wrapper.find('#field-enable_sni').element as HTMLInputElement).checked).toBe(true)
  })

  it('saves only the changed section and maps the checkbox back to y/n', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.find('#field-website_path').setValue('/srv/www/$domain')
    await wrapper.find('#field-enable_sni').setValue(false)

    fetchMock.mockResolvedValueOnce(
      res(200, { website_path: '/srv/www/$domain', enable_sni: 'n' }),
    )
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const puts = fetchMock.mock.calls.filter((c) => c[1]?.method === 'PUT')
    expect(puts).toHaveLength(1)
    expect(puts[0][0]).toBe('/api/servers/1/config/web')
    expect(JSON.parse(puts[0][1].body)).toEqual({
      website_path: '/srv/www/$domain',
      enable_sni: 'n',
    })
    expect(wrapper.find('[data-test="saved"]').exists()).toBe(true)
  })

  it('does not PUT anything when nothing changed', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(fetchMock.mock.calls.filter((c) => c[1]?.method === 'PUT')).toHaveLength(0)
  })

  it('a key absent from the stored INI does not by itself dirty its section', async () => {
    // enable_sni is missing here: the field shows its default ('y'), which is
    // what getconf already applies — saving must not materialise it.
    fetchMock.mockResolvedValueOnce(res(200, meta))
    fetchMock.mockResolvedValueOnce(res(200, { web: { website_path: '/var/www/$domain' }, dns: {} }))
    fetchMock.mockResolvedValueOnce(res(200, {}))
    const wrapper = mount(ServerConfigView)
    await flushPromises()

    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(fetchMock.mock.calls.filter((c) => c[1]?.method === 'PUT')).toHaveLength(0)
  })
})
