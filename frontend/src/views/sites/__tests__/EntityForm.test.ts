// Metadata-driven entity form: tabs/fields rendered from the form metadata
// endpoint, value conversion (y/n checkboxes), create/update submission and
// inline 422 mapping with the offending tab kept active. fetch is mocked.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import EntityForm from '../EntityForm.vue'

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

// A trimmed web-domains metadata: two tabs, one field each.
const meta = {
  name: 'web-domains',
  title: 'web_vhost_domain_edit_title',
  tabs: [
    {
      name: 'domain',
      label: 'domain_tab_txt',
      fields: [
        {
          name: 'domain',
          label: 'domain_txt',
          type: 'text',
          datatype: 'VARCHAR',
          formtype: 'TEXT',
        },
        {
          name: 'active',
          label: 'active_txt',
          type: 'checkbox',
          datatype: 'VARCHAR',
          formtype: 'CHECKBOX',
          default: 'y',
          options: [
            { value: 'n', label: 'no_txt' },
            { value: 'y', label: 'yes_txt' },
          ],
        },
      ],
    },
    {
      name: 'redirect',
      label: 'redirect_tab_txt',
      fields: [
        {
          name: 'redirect_path',
          label: 'redirect_path_txt',
          type: 'text',
          datatype: 'VARCHAR',
          formtype: 'TEXT',
        },
      ],
    },
  ],
}

const domainProps = {
  entity: 'web-domains',
  apiBase: '/api/sites/web-domains',
  backTo: '/sites',
}

beforeEach(() => {
  fetchMock.mockReset()
  push.mockReset()
})

describe('EntityForm', () => {
  it('renders tabs and fields from the metadata endpoint', async () => {
    fetchMock.mockResolvedValueOnce(res(200, meta))
    const wrapper = mount(EntityForm, { props: domainProps })
    await flushPromises()

    expect(fetchMock.mock.calls[0][0]).toBe('/api/meta/forms/web-domains')
    expect(wrapper.text()).toContain('Domain')
    expect(wrapper.text()).toContain('Redirect')
    expect(wrapper.find('#field-domain').exists()).toBe(true)
    expect((wrapper.find('#field-active').element as HTMLInputElement).checked).toBe(
      true,
      // checkbox default 'y' converted to a checked box
    )
  })

  it('creates a record with y/n checkbox values and returns to the list', async () => {
    fetchMock.mockResolvedValueOnce(res(200, meta)).mockResolvedValueOnce(res(201, { domain_id: 5 }))
    const wrapper = mount(EntityForm, { props: domainProps })
    await flushPromises()

    await wrapper.find('#field-domain').setValue('example.com')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const [url, init] = fetchMock.mock.calls[1]
    expect(url).toBe('/api/sites/web-domains')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body)).toMatchObject({ domain: 'example.com', active: 'y' })
    expect(push).toHaveBeenCalledWith('/sites')
  })

  it('loads the record on edit, PUTs changes and shows the datalog state', async () => {
    fetchMock
      .mockResolvedValueOnce(res(200, meta))
      .mockResolvedValueOnce(
        res(200, {
          domain_id: 5,
          domain: 'example.com',
          active: 'n',
          _datalog_state: 'error',
          _datalog_error: 'nginx: -t failed',
        }),
      )
      .mockResolvedValueOnce(res(200, { domain_id: 5 }))
    const wrapper = mount(EntityForm, { props: { ...domainProps, id: '5' } })
    await flushPromises()

    expect(fetchMock.mock.calls[1][0]).toBe('/api/sites/web-domains/5')
    expect((wrapper.find('#field-domain').element as HTMLInputElement).value).toBe('example.com')
    expect((wrapper.find('#field-active').element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.find('[data-test="state-error"]').text()).toContain('nginx: -t failed')

    await wrapper.find('form').trigger('submit')
    await flushPromises()
    const [url, init] = fetchMock.mock.calls[2]
    expect(url).toBe('/api/sites/web-domains/5')
    expect(init.method).toBe('PUT')
  })

  it('maps 422 field errors inline and switches to the offending tab', async () => {
    fetchMock
      .mockResolvedValueOnce(res(200, meta))
      .mockResolvedValueOnce(
        res(422, {
          error: {
            key: 'error.validation_failed',
            fields: { redirect_path: ['redirect_error_regex'] },
          },
        }),
      )
    const wrapper = mount(EntityForm, { props: domainProps })
    await flushPromises()

    await wrapper.find('form').trigger('submit')
    await flushPromises()

    // The redirect tab (holding the offending field) is now active and the
    // translated message is shown at the field.
    expect(wrapper.find('#field-redirect_path').isVisible()).toBe(true)
    expect(wrapper.text()).toContain('Invalid redirect path')
    expect(push).not.toHaveBeenCalled()
  })
})
