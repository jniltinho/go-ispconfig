<script setup lang="ts">
// Demo page exercising the DataTable and TabbedForm primitives with mock
// data until the real module list/form endpoints land.
import { computed, ref } from 'vue'
import { utilityIcons } from '../icons'
import DataTable, { type Column, type Row } from '../components/DataTable.vue'
import TabbedForm, { type FormMetadata } from '../components/TabbedForm.vue'
import { useI18n } from '../i18n'

const { t } = useI18n()

// --- DataTable demo (server-side pagination simulated on mock rows) ---
const columns: Column[] = [
  { key: 'id', label: 'ID' },
  { key: 'active', label: t('demo.active') },
  { key: 'server', label: 'Server' },
  { key: 'domain', label: 'Domain' },
]

const allRows: Row[] = Array.from({ length: 12 }, (_, i) => ({
  id: i + 1,
  active: i % 3 !== 0 ? t('demo.active') : t('demo.inactive'),
  server: 'server1.example.com',
  domain: `site${i + 1}.example.com`,
}))

const page = ref(1)
const pageSize = 5
const filters = ref<Record<string, string>>({})

const filtered = computed(() =>
  allRows.filter((row) =>
    Object.entries(filters.value).every(([key, value]) =>
      String(row[key]).toLowerCase().includes(value.toLowerCase()),
    ),
  ),
)
const rows = computed(() => filtered.value.slice((page.value - 1) * pageSize, page.value * pageSize))

function onFilter(next: Record<string, string>) {
  filters.value = next
  page.value = 1
}

// --- TabbedForm demo ---
const metadata: FormMetadata = {
  tabs: [
    {
      name: 'domain',
      label: 'Domain',
      fields: [
        { name: 'domain', type: 'text', label: 'Domain' },
        {
          name: 'server_id',
          type: 'select',
          label: 'Server',
          options: [{ value: '1', label: 'server1.example.com' }],
          default: '1',
        },
        { name: 'active', type: 'checkbox', label: t('demo.active'), default: true },
      ],
    },
    {
      name: 'options',
      label: 'Options',
      fields: [{ name: 'notes', type: 'textarea', label: 'Notes' }],
    },
  ],
}

const savedValues = ref<Record<string, unknown> | null>(null)
</script>

<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-lg font-bold">{{ t('demo.websites_title') }}</h1>
      <p class="mb-3 text-sm">{{ t('demo.websites_description') }}</p>
      <button
        type="button"
        class="mb-3 btn btn-success px-4 py-2"
      >
        {{ t('demo.add_new') }}
      </button>
      <DataTable
        :columns="columns"
        :rows="rows"
        :total="filtered.length"
        :page="page"
        :page-size="pageSize"
        has-actions
        @update:page="page = $event"
        @filter="onFilter"
      >
        <template #actions>
          <button type="button" class="border border-border bg-surface p-1 hover:bg-info">
            <component :is="utilityIcons.edit" :size="14" />
          </button>
          <button type="button" class="ml-1 border border-danger-border bg-danger p-1 text-danger-text">
            <component :is="utilityIcons.delete" :size="14" />
          </button>
        </template>
      </DataTable>
    </div>

    <div>
      <h1 class="mb-3 text-lg font-bold">{{ t('demo.form_title') }}</h1>
      <TabbedForm :metadata="metadata" @save="savedValues = $event" @cancel="savedValues = null" />
      <pre v-if="savedValues" class="mt-3 border border-border bg-surface p-3 text-xs">{{
        savedValues
      }}</pre>
    </div>
  </div>
</template>
