// Protected Folders list: rows from /api/sites/web-folders, Add button and
// the per-folder Users navigation. fetch and the router are mocked.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import WebFolderList from '../WebFolderList.vue'
import WebFolderUserList from '../WebFolderUserList.vue'

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

beforeEach(() => {
  setActivePinia(createPinia())
  fetchMock.mockReset()
  push.mockReset()
})

describe('WebFolderList', () => {
  const page = {
    items: [{ web_folder_id: 7, active: 'y', server_id: 1, parent_domain_id: 3, path: 'protected' }],
    total: 1,
    page: 1,
    limit: 25,
  }

  it('renders folders and navigates to edit, users and create', async () => {
    fetchMock.mockResolvedValue(res(200, page))
    const wrapper = mount(WebFolderList)
    await flushPromises()

    expect(fetchMock.mock.calls[0][0]).toBe('/api/sites/web-folders?page=1&limit=25')
    expect(wrapper.text()).toContain('protected')

    await wrapper.find('[data-test="folder-users"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/sites/folders/7/users')

    await wrapper.find('tbody tr').trigger('click')
    expect(push).toHaveBeenCalledWith('/sites/folders/7')

    await wrapper.find('button').trigger('click') // Add button (first button)
    expect(push).toHaveBeenCalledWith('/sites/folders/new')
  })
})

describe('WebFolderUserList', () => {
  it('lists the users of one folder and navigates to create', async () => {
    fetchMock.mockResolvedValue(
      res(200, {
        items: [{ web_folder_user_id: 9, active: 'y', username: 'user1' }],
        total: 1,
        page: 1,
        limit: 25,
      }),
    )
    const wrapper = mount(WebFolderUserList, { props: { folderId: '7' } })
    await flushPromises()

    expect(fetchMock.mock.calls[0][0]).toBe(
      '/api/sites/web-folder-users?page=1&limit=25&web_folder_id=7',
    )
    expect(wrapper.text()).toContain('user1')

    await wrapper.find('button').trigger('click')
    expect(push).toHaveBeenCalledWith('/sites/folders/7/users/new')

    await wrapper.find('tbody tr').trigger('click')
    expect(push).toHaveBeenCalledWith('/sites/folders/7/users/9')
  })
})
