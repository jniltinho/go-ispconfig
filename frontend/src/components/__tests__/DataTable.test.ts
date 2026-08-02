import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import DataTable, { type Column } from '../DataTable.vue'

const columns: Column[] = [
  { key: 'id', label: 'ID' },
  { key: 'domain', label: 'Domain' },
]

describe('DataTable', () => {
  it('renders rows and pagination info', () => {
    const wrapper = mount(DataTable, {
      props: {
        columns,
        rows: [
          { id: 1, domain: 'a.example.com' },
          { id: 2, domain: 'b.example.com' },
        ],
        total: 12,
        page: 1,
        pageSize: 5,
      },
    })
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    expect(wrapper.text()).toContain('a.example.com')
    expect(wrapper.text()).toContain('12 records')
    expect(wrapper.text()).toContain('Page 1 of 3')
  })

  it('shows the empty state when there are no rows', () => {
    const wrapper = mount(DataTable, {
      props: { columns, rows: [], total: 0, page: 1, pageSize: 5 },
    })
    expect(wrapper.text()).toContain('No records found.')
  })

  it('emits update:page on next', async () => {
    const wrapper = mount(DataTable, {
      props: { columns, rows: [{ id: 1, domain: 'x' }], total: 12, page: 1, pageSize: 5 },
    })
    const buttons = wrapper.findAll('tfoot button')
    await buttons[1].trigger('click')
    expect(wrapper.emitted('update:page')).toEqual([[2]])
  })

  it('emits filter with non-empty values on enter', async () => {
    const wrapper = mount(DataTable, {
      props: { columns, rows: [], total: 0, page: 1, pageSize: 5 },
    })
    const input = wrapper.findAll('thead input')[1]
    await input.setValue('example')
    await input.trigger('keyup.enter')
    expect(wrapper.emitted('filter')).toEqual([[{ domain: 'example' }]])
  })

  it('renders skeleton rows while loading and hides data', () => {
    const wrapper = mount(DataTable, {
      props: {
        columns,
        rows: [{ id: 1, domain: 'a.example.com' }],
        total: 1,
        page: 1,
        pageSize: 5,
        loading: true,
      },
    })
    expect(wrapper.findAll('[data-test="skeleton-row"]')).toHaveLength(5)
    expect(wrapper.text()).not.toContain('a.example.com')
  })

  it('empty state shows icon and hint text', () => {
    const wrapper = mount(DataTable, {
      props: { columns, rows: [], total: 0, page: 1, pageSize: 5 },
    })
    const empty = wrapper.find('[data-test="empty-state"]')
    expect(empty.exists()).toBe(true)
    expect(empty.text()).toContain('No records found.')
    expect(empty.text()).toContain('Add a new record')
    expect(empty.find('svg').exists()).toBe(true)
  })

  it('filtered empty state hints at clearing filters', async () => {
    const wrapper = mount(DataTable, {
      props: { columns, rows: [], total: 0, page: 1, pageSize: 5 },
    })
    await wrapper.find('thead input').setValue('nope')
    expect(wrapper.find('[data-test="empty-state"]').text()).toContain('clearing the column filters')
  })

  it('action-less tables still expose the filter button', async () => {
    const wrapper = mount(DataTable, {
      props: { columns, rows: [], total: 0, page: 1, pageSize: 5 },
    })
    const btn = wrapper.find('thead button[aria-label="Filter"]')
    expect(btn.exists()).toBe(true)
    await wrapper.find('thead input').setValue('abc')
    await btn.trigger('click')
    expect(wrapper.emitted('filter')).toEqual([[{ id: 'abc' }]])
  })
})
