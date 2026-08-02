// Migration wizard: connect step (panel info, fault, missing grants),
// inventory with the multi-server guard, dry-run rendering and the
// status-reattach on mount. fetch is mocked.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import MigrationWizard from '../MigrationWizard.vue'

const fetchMock = vi.fn()
vi.stubGlobal('fetch', fetchMock)
vi.stubGlobal('EventSource', undefined)

function res(status: number, body: unknown = '') {
  const text = typeof body === 'string' ? body : JSON.stringify(body)
  return { ok: status >= 200 && status < 300, status, text: async () => text }
}

// idle status for the reattach call on mount
const idle = res(200, { state: 'idle' })

const inventory = {
  clients: 3,
  web_domains: 1201,
  web_folders: 1,
  web_folder_users: 1,
  dns_zones: 2,
  dns_records: 4,
  dns_slave_zones: 1,
  dns_templates: 1,
  servers: [
    { server_id: '1', server_name: 'legacy1' },
    { server_id: '2', server_name: 'legacy2' },
  ],
  multi_server: true,
}

beforeEach(() => {
  fetchMock.mockReset()
})

async function mountWizard() {
  fetchMock.mockResolvedValueOnce(idle)
  const wrapper = mount(MigrationWizard)
  await flushPromises()
  return wrapper
}

describe('MigrationWizard', () => {
  it('shows panel info after a successful connection test', async () => {
    const wrapper = await mountWizard()
    fetchMock.mockResolvedValueOnce(
      res(200, {
        servers: [{ server_id: '1', server_name: 'legacy1' }],
        multi_server: false,
        insecure: false,
        plain_http: true,
      }),
    )
    await wrapper.find('[data-test="mig-url"]').setValue('http://legacy:8080')
    await wrapper.find('[data-test="mig-user"]').setValue('migrator')
    await wrapper.find('[data-test="mig-pass"]').setValue('pw')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const info = wrapper.find('[data-test="mig-panel-info"]')
    expect(info.exists()).toBe(true)
    expect(info.text()).toContain('legacy1')
    expect(info.text()).toContain('unencrypted')
    const body = JSON.parse(fetchMock.mock.calls[1][1].body as string)
    expect(body.password).toBe('pw')
  })

  it('renders the exact missing grants from a connect failure', async () => {
    const wrapper = await mountWizard()
    fetchMock.mockResolvedValueOnce(
      res(400, { error: 'grant preflight failed', missing_functions: ['dns_zone_get', 'client_get'] }),
    )
    await wrapper.find('[data-test="mig-pass"]').setValue('pw')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const err = wrapper.find('[data-test="migration-error"]')
    expect(err.text()).toContain('dns_zone_get')
    expect(err.text()).toContain('client_get')
  })

  it('shows the legacy fault code on login failure', async () => {
    const wrapper = await mountWizard()
    fetchMock.mockResolvedValueOnce(res(400, { error: 'login failed', fault_code: 'remote_fault' }))
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-test="migration-error"]').text()).toContain('remote_fault')
  })

  it('multi-server inventory blocks dry-run until explicitly confirmed', async () => {
    const wrapper = await mountWizard()
    fetchMock.mockResolvedValueOnce(res(200, { servers: inventory.servers, multi_server: true, insecure: false, plain_http: false }))
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    fetchMock.mockResolvedValueOnce(res(200, inventory))
    await wrapper.find('[data-test="mig-to-inventory"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="mig-inventory"]').text()).toContain('1201')
    const guard = wrapper.find('[data-test="mig-multiserver"]')
    expect(guard.exists()).toBe(true)
    expect(guard.text()).toContain('legacy2')

    const next = wrapper.find('[data-test="mig-to-dryrun"]')
    expect(next.attributes('disabled')).toBeDefined()
    await wrapper.find('[data-test="mig-confirm-multi"]').setValue(true)
    expect(next.attributes('disabled')).toBeUndefined()
  })

  it('dry-run renders counts and conflict reasons', async () => {
    const wrapper = await mountWizard()
    fetchMock.mockResolvedValueOnce(res(200, { servers: [{ server_id: '1', server_name: 'l1' }], multi_server: false, insecure: false, plain_http: false }))
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    fetchMock.mockResolvedValueOnce(res(200, { ...inventory, multi_server: false }))
    await wrapper.find('[data-test="mig-to-inventory"]').trigger('click')
    await flushPromises()

    fetchMock.mockResolvedValueOnce(
      res(200, {
        counts: { web_domain: { created: 1200, updated: 0, skipped: 0, conflicts: 1 } },
        conflicts: [
          { table: 'web_domain', key: 'example.com (vhost)', action: 'conflict', reason: 'owned by a different user' },
        ],
        warnings: [],
        reset_required: ['reseller1'],
      }),
    )
    await wrapper.find('[data-test="mig-to-dryrun"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="mig-plan"]').text()).toContain('1200')
    const conflicts = wrapper.find('[data-test="mig-conflicts"]')
    expect(conflicts.text()).toContain('owned by a different user')
    // Selection toggles travel with the dry-run request.
    const body = JSON.parse(fetchMock.mock.calls.at(-1)![1].body as string)
    expect(body.selection).toEqual({ clients: true, sites: true, dns: true })
  })

  it('reattaches to a finished run via status on mount', async () => {
    fetchMock.mockResolvedValueOnce(
      res(200, {
        state: 'done',
        report: {
          counts: { client: { created: 3, updated: 0, skipped: 0, conflicts: 0 } },
          conflicts: [],
          reset_required: ['reseller1', 'client2'],
          warnings: ['certificates must be re-issued'],
          rsync_suggestions: ['rsync -a --usermap=*:web1 legacy:/var/www/x/ /var/www/x/'],
          operational_order: ['1. Wait for the daemon.'],
        },
      }),
    )
    const wrapper = mount(MigrationWizard)
    await flushPromises()

    const report = wrapper.find('[data-test="mig-report"]')
    expect(report.exists()).toBe(true)
    expect(report.text()).toContain('re-issued')
    expect(wrapper.find('[data-test="mig-reset"]').text()).toContain('reseller1')
    expect(wrapper.find('[data-test="mig-rsync"]').text()).toContain('--usermap')
  })

  it('generates one-time reset tokens from the report', async () => {
    fetchMock.mockResolvedValueOnce(
      res(200, {
        state: 'done',
        report: {
          counts: {},
          conflicts: [],
          reset_required: ['reseller1'],
          warnings: [],
          rsync_suggestions: [],
          operational_order: [],
        },
      }),
    )
    const wrapper = mount(MigrationWizard)
    await flushPromises()

    fetchMock.mockResolvedValueOnce(res(200, [{ username: 'reseller1', token: 'aabbccdd' }]))
    await wrapper.find('[data-test="mig-reset-generate"]').trigger('click')
    await flushPromises()

    const tokens = wrapper.find('[data-test="mig-reset-tokens"]')
    expect(tokens.text()).toContain('reseller1')
    expect(tokens.text()).toContain('aabbccdd')
  })
})
