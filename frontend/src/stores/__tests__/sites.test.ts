// Sites store: server-side list state for the domain DataTable. fetch is
// mocked.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { listQuery, useSitesStore } from '../sites'

const fetchMock = vi.fn()
vi.stubGlobal('fetch', fetchMock)

function res(status: number, body: unknown = '') {
  const text = typeof body === 'string' ? body : JSON.stringify(body)
  return { ok: status >= 200 && status < 300, status, text: async () => text }
}

beforeEach(() => {
  setActivePinia(createPinia())
  fetchMock.mockReset()
})

describe('listQuery', () => {
  it('builds page/limit plus non-empty filters', () => {
    expect(listQuery(2, 25, { domain: 'exa', type: '' })).toBe('page=2&limit=25&domain=exa')
  })
})

describe('sites store', () => {
  it('loads a domain page from /api/sites/web-domains', async () => {
    fetchMock.mockResolvedValueOnce(
      res(200, {
        items: [{ domain_id: 1, domain: 'example.com', _datalog_state: 'pending' }],
        total: 1,
        page: 1,
        limit: 25,
      }),
    )
    const store = useSitesStore()
    store.filters = { domain: 'exa' }
    await store.loadDomains(1)

    expect(fetchMock.mock.calls[0][0]).toBe('/api/sites/web-domains?page=1&limit=25&domain=exa')
    expect(store.domains).toHaveLength(1)
    expect(store.total).toBe(1)
    expect(store.error).toBe('')
  })

  it('stores an i18n error key on failure', async () => {
    fetchMock.mockResolvedValueOnce(res(403, { error: { key: 'error.forbidden' } }))
    const store = useSitesStore()
    await store.loadDomains(1)
    expect(store.error).toBe('error.forbidden')
    expect(store.loading).toBe(false)
  })

  it('fetches a cron page and its run history through fetchList', async () => {
    fetchMock.mockResolvedValueOnce(res(200, { items: [{ id: 7 }], total: 1, page: 1, limit: 25 }))
    const store = useSitesStore()
    const crons = await store.fetchList('/api/sites/crons', 1, 25, { active: 'y' })
    expect(fetchMock.mock.calls[0][0]).toBe('/api/sites/crons?page=1&limit=25&active=y')
    expect(crons.items).toHaveLength(1)

    fetchMock.mockResolvedValueOnce(
      res(200, { items: [{ syslog_id: 3, status: 'ok' }], total: 1, page: 1, limit: 25 }),
    )
    const runs = await store.fetchList('/api/sites/crons/7/runs', 1, 25)
    expect(fetchMock.mock.calls[1][0]).toBe('/api/sites/crons/7/runs?page=1&limit=25')
    expect(runs.items[0].status).toBe('ok')
  })

  it('rejects with the API error key when the cron list is forbidden', async () => {
    fetchMock.mockResolvedValueOnce(res(403, { error: { key: 'error.forbidden' } }))
    const store = useSitesStore()
    await expect(store.fetchList('/api/sites/crons', 1, 25)).rejects.toMatchObject({
      key: 'error.forbidden',
    })
  })
})
