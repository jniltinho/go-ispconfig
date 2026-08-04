// Mail domain form: generating a DKIM key fills the private/public fields
// and renders the DNS-Record right away, before the record is saved
// (parity with legacy mail_domain_edit.htm getDKIM()). fetch is mocked.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import DomainForm from '../DomainForm.vue'

vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))

const fetchMock = vi.fn()
vi.stubGlobal('fetch', fetchMock)

function res(status: number, body: unknown = '') {
  const text = typeof body === 'string' ? body : JSON.stringify(body)
  return { ok: status >= 200 && status < 300, status, text: async () => text }
}

const meta = {
  name: 'domains',
  title: 'mail_domain_edit_title',
  tabs: [
    {
      name: 'domain',
      label: 'domain_tab_txt',
      fields: [
        { name: 'domain', label: 'domain_txt', type: 'text', datatype: 'VARCHAR', formtype: 'TEXT' },
        {
          name: 'dkim_selector',
          label: 'dkim_selector_txt',
          type: 'text',
          datatype: 'VARCHAR',
          formtype: 'TEXT',
          default: 'default',
        },
        {
          name: 'dkim_private',
          label: 'dkim_private_txt',
          type: 'textarea',
          datatype: 'TEXT',
          formtype: 'TEXTAREA',
        },
        {
          name: 'dkim_public',
          label: 'dkim_public_txt',
          type: 'textarea',
          datatype: 'TEXT',
          formtype: 'TEXTAREA',
        },
      ],
    },
  ],
}

const PUB = '-----BEGIN PUBLIC KEY-----\nMIIBIjANBgkq\nAQAB\n-----END PUBLIC KEY-----\n'

describe('DomainForm DKIM panel', () => {
  beforeEach(() => {
    fetchMock.mockReset()
  })

  it('renders the DNS-Record after generating a key on a new domain', async () => {
    fetchMock
      .mockResolvedValueOnce(res(200, [])) // spamfilter policies lookup
      .mockResolvedValueOnce(res(200, meta)) // form metadata
      .mockResolvedValueOnce(res(200, { dkim_private: 'PRIV', dkim_public: PUB }))
    const wrapper = mount(DomainForm, { attachTo: document.body })
    await flushPromises()

    await wrapper.find('#field-domain').setValue('linuxpro.com.br')
    await wrapper.find('[data-test="dkim-generate"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="dkim-suggested"]').text()).toBe(
      'default._domainkey.linuxpro.com.br. 3600   IN\tTXT\t"v=DKIM1; t=s; p=MIIBIjANBgkqAQAB"',
    )
    wrapper.unmount()
  })

  it('keeps the DNS-Record hidden while the domain is empty', async () => {
    fetchMock
      .mockResolvedValueOnce(res(200, []))
      .mockResolvedValueOnce(res(200, meta))
      .mockResolvedValueOnce(res(200, { dkim_private: 'PRIV', dkim_public: PUB }))
    const wrapper = mount(DomainForm, { attachTo: document.body })
    await flushPromises()

    await wrapper.find('[data-test="dkim-generate"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="dkim-suggested"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
