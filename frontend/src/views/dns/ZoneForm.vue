<script setup lang="ts">
// DNS zone form (port of dns_soa.tform.php, design D10): tab Records
// (default) with the embedded record grid, tab Zone settings with the
// metadata-driven SOA form (update_acl only reaches admins — the API strips
// it from non-admin metadata), tab Zone rendering with the read-only
// rendered_zone cache. DNSSEC info is shown read-only under the settings.
import { onMounted, ref } from 'vue'
import { api, ApiError } from '../../api'
import UiAlert from '../../components/UiAlert.vue'
import { useI18n } from '../../i18n'
import EntityForm from '../sites/EntityForm.vue'
import RecordGrid from './RecordGrid.vue'

const props = defineProps<{ id: string }>()

const { t } = useI18n()

const tabs = [
  { name: 'records', label: 'dns.tab.records' },
  { name: 'settings', label: 'dns.tab.settings' },
  { name: 'rendering', label: 'dns.tab.rendering' },
]
const activeTab = ref('records')

const origin = ref('')
const renderedZone = ref('')
const dnssecInfo = ref('')
const datalogState = ref('')
const datalogError = ref('')
const loadError = ref('')

async function loadZone() {
  try {
    const rec = await api.get<Record<string, unknown>>(`/api/dns/zones/${props.id}`)
    origin.value = String(rec.origin ?? '')
    renderedZone.value = String(rec.rendered_zone ?? '')
    dnssecInfo.value = String(rec.dnssec_info ?? '')
    datalogState.value = String(rec._datalog_state ?? '')
    datalogError.value = String(rec._datalog_error ?? '')
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
}

onMounted(loadZone)
</script>

<template>
  <div>
    <h1 class="mb-3 text-lg font-bold">{{ t('dns_soa_edit_title') }} {{ origin }}</h1>

    <p
      v-if="datalogState === 'pending'"
      class="mb-3 border border-border bg-info px-3 py-2 text-sm"
      data-test="state-pending"
    >
      {{ t('sites.state.pending') }}
    </p>
    <p
      v-else-if="datalogState === 'error'"
      class="mb-3 border border-danger-border bg-danger px-3 py-2 text-sm text-danger-text"
      data-test="state-error"
    >
      {{ t('sites.state.error') }}: {{ datalogError }}
    </p>
    <UiAlert v-if="loadError" variant="danger" class="mb-3" :messages="[t(loadError)]" />

    <!-- Top-level tabs (Records / Zone settings / Zone rendering) -->
    <div class="flex border border-border border-b-0 bg-bg">
      <button
        v-for="tab in tabs"
        :key="tab.name"
        type="button"
        :data-test="`zone-tab-${tab.name}`"
        class="border-r border-border px-5 py-2.5 text-sm font-bold"
        :class="activeTab === tab.name ? 'bg-surface text-text' : 'text-text/70 hover:bg-info'"
        @click="activeTab = tab.name"
      >
        {{ t(tab.label) }}
      </button>
    </div>

    <div class="border border-border bg-surface p-4">
      <div v-show="activeTab === 'records'">
        <RecordGrid :zone-id="id" @changed="loadZone" />
      </div>

      <div v-if="activeTab === 'settings'">
        <EntityForm
          entity="zones"
          api-base="/api/dns/zones"
          back-to="/dns"
          :id="id"
          embedded
          :readonly-fields="['serial']"
        />
        <div v-if="dnssecInfo" class="mt-4">
          <h2 class="mb-1 text-sm font-bold">{{ t('dns.dnssec_info') }}</h2>
          <textarea
            readonly
            rows="8"
            data-test="dnssec-info"
            class="w-full border border-border bg-bg px-3 py-1.5 font-mono text-xs"
            :value="dnssecInfo"
          />
        </div>
      </div>

      <div v-if="activeTab === 'rendering'">
        <textarea
          readonly
          rows="24"
          data-test="rendered-zone"
          class="w-full border border-border bg-bg px-3 py-1.5 font-mono text-xs"
          :value="renderedZone || t('dns.no_rendered_zone')"
        />
      </div>
    </div>
  </div>
</template>
