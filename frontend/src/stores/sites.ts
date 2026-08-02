// Pinia store of the Sites module: server-side list state for the domain
// DataTable plus a generic paginated fetch shared by the folder views.
import { defineStore } from 'pinia'
import { api, ApiError } from '../api'

/** ListResponse is the paginated body of every entity list endpoint. */
export interface ListResponse {
  items: Record<string, unknown>[]
  total: number
  page: number
  limit: number
}

/** listQuery builds the ?page=&limit=&<field>= query string of a list call. */
export function listQuery(
  page: number,
  limit: number,
  filters: Record<string, string> = {},
): string {
  const params = new URLSearchParams({ page: String(page), limit: String(limit) })
  for (const [key, value] of Object.entries(filters)) {
    if (value !== '') params.set(key, value)
  }
  return params.toString()
}

export const useSitesStore = defineStore('sites', {
  state: () => ({
    domains: [] as Record<string, unknown>[],
    total: 0,
    page: 1,
    pageSize: 25,
    filters: {} as Record<string, string>,
    loading: false,
    error: '' as string,
  }),
  actions: {
    /** fetchList is the generic paginated list call used by every sites view. */
    async fetchList(
      path: string,
      page: number,
      limit: number,
      filters: Record<string, string> = {},
    ): Promise<ListResponse> {
      return api.get<ListResponse>(`${path}?${listQuery(page, limit, filters)}`)
    },
    /** loadDomains refreshes the domain DataTable page under the current filters. */
    async loadDomains(page?: number): Promise<void> {
      this.loading = true
      this.error = ''
      try {
        const res = await this.fetchList(
          '/api/sites/web-domains',
          page ?? this.page,
          this.pageSize,
          this.filters,
        )
        this.domains = res.items
        this.total = res.total
        this.page = res.page
      } catch (e) {
        this.error = e instanceof ApiError ? e.key : 'error.request_failed'
      } finally {
        this.loading = false
      }
    },
  },
})
