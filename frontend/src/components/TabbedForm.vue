<script setup lang="ts">
// ISPConfig-style tabbed form: flat tabs on top of a white card, fields
// rendered from JSON form metadata, labels on the left with an automatic ':',
// green Save / Cancel buttons and per-field errors in alert-danger style.
import { reactive, ref, watch } from 'vue'
import UiAlert from './UiAlert.vue'
import { useI18n } from '../i18n'

export interface FormField {
  name: string
  type: 'text' | 'password' | 'textarea' | 'select' | 'checkbox' | 'legend'
  label: string
  options?: { value: string; label: string }[]
  default?: unknown
  /** readonly renders the field disabled (e.g. server-managed serial). */
  readonly?: boolean
}

export interface FormTab {
  name: string
  label: string
  fields: FormField[]
}

export interface FormMetadata {
  tabs: FormTab[]
}

const props = defineProps<{
  metadata: FormMetadata
  modelValue?: Record<string, unknown>
  errors?: Record<string, string[]>
  /** saving disables Save/Cancel while the request is in flight. */
  saving?: boolean
}>()

const emit = defineEmits<{
  (e: 'save', values: Record<string, unknown>): void
  (e: 'cancel'): void
  (e: 'values-change', values: Record<string, unknown>): void
}>()

const { t } = useI18n()
const activeTab = ref(props.metadata.tabs[0]?.name ?? '')

// Working copy of the values: modelValue > field default.
const values = reactive<Record<string, unknown>>({})
for (const tab of props.metadata.tabs) {
  for (const field of tab.fields) {
    if (field.type === 'legend') continue
    values[field.name] =
      props.modelValue?.[field.name] ??
      field.default ??
      (field.type === 'checkbox' ? false : '')
  }
}

// fieldLabel resolves a field name to its display label for the error
// summary (falls back to the raw name for fixed/server-only fields).
function fieldLabel(name: string): string {
  for (const tab of props.metadata.tabs) {
    const field = tab.fields.find((f) => f.name === name)
    if (field?.label) return field.label
  }
  return name
}

const errorList = () => Object.entries(props.errors ?? {}).filter(([, msgs]) => msgs.length > 0)

watch(
  values,
  (v) => emit('values-change', { ...v }),
  { deep: true },
)

// When server validation errors arrive, stay on (or jump to) the first tab
// holding an offending field so the inline message is visible.
watch(
  () => props.errors,
  (errors) => {
    const bad = new Set(
      Object.entries(errors ?? {})
        .filter(([, msgs]) => msgs.length > 0)
        .map(([field]) => field),
    )
    if (bad.size === 0) return
    const hasBadField = (name: string) =>
      props.metadata.tabs.find((tab) => tab.name === name)?.fields.some((f) => bad.has(f.name))
    if (hasBadField(activeTab.value)) return
    const offending = props.metadata.tabs.find((tab) => tab.fields.some((f) => bad.has(f.name)))
    if (offending) activeTab.value = offending.name
  },
  { immediate: true },
)
</script>

<template>
  <form
    class="border border-border bg-surface"
    data-test="tabbed-form"
    @submit.prevent="emit('save', { ...values })"
  >
    <!-- Flat tabs -->
    <div role="tablist" class="flex border-b border-border bg-bg">
      <button
        v-for="tab in metadata.tabs"
        :key="tab.name"
        type="button"
        role="tab"
        :aria-selected="activeTab === tab.name"
        :aria-controls="`tabpanel-${tab.name}`"
        class="-mb-px border-r border-border px-4 py-2 text-sm font-bold transition-colors duration-150"
        :class="
          activeTab === tab.name
            ? 'border-b border-b-surface bg-surface text-text'
            : 'border-b border-b-border text-text-muted hover:bg-info'
        "
        @click="activeTab = tab.name"
      >
        {{ tab.label }}
      </button>
    </div>

    <!-- Error summary (alert-danger composition) -->
    <UiAlert
      v-if="errorList().length"
      variant="danger"
      class="m-4"
      :messages="errorList().map(([field, msgs]) => `${fieldLabel(field)}: ${msgs.join(', ')}`)"
    />

    <!-- Fields of the active tab -->
    <div class="px-4 py-4">
      <template v-for="tab in metadata.tabs" :key="tab.name">
        <div
          v-show="activeTab === tab.name"
          :id="`tabpanel-${tab.name}`"
          role="tabpanel"
          class="mx-auto max-w-xl space-y-2"
        >
          <template v-for="field in tab.fields" :key="field.name">
            <!-- Fieldset legend as a sub-heading (original trait) -->
            <p
              v-if="field.type === 'legend'"
              class="border-b border-border pb-1 text-sm font-bold text-text"
            >
              {{ field.label }}
            </p>
            <div v-else class="flex items-start gap-3">
            <label
              :for="`field-${field.name}`"
              class="w-44 shrink-0 pt-1 text-right text-sm font-semibold after:content-[':']"
            >
              {{ field.label }}
            </label>
            <div class="flex-1">
              <input
                v-if="field.type === 'text' || field.type === 'password'"
                :id="`field-${field.name}`"
                v-model="values[field.name] as string"
                :type="field.type"
                :disabled="field.readonly"
                class="w-full border border-border bg-surface px-2 py-1 text-sm outline-none focus:border-link disabled:bg-bg disabled:text-text/60"
                :class="{ 'border-danger-border': errors?.[field.name]?.length }"
              />
              <textarea
                v-else-if="field.type === 'textarea'"
                :id="`field-${field.name}`"
                v-model="values[field.name] as string"
                rows="4"
                :disabled="field.readonly"
                class="w-full border border-border bg-surface px-2 py-1 text-sm outline-none focus:border-link disabled:bg-bg disabled:text-text/60"
                :class="{ 'border-danger-border': errors?.[field.name]?.length }"
              />
              <select
                v-else-if="field.type === 'select'"
                :id="`field-${field.name}`"
                v-model="values[field.name] as string"
                :disabled="field.readonly"
                class="w-full border border-border bg-surface px-2 py-1 text-sm outline-none focus:border-link disabled:bg-bg disabled:text-text/60"
                :class="{ 'border-danger-border': errors?.[field.name]?.length }"
              >
                <option v-for="opt in field.options" :key="opt.value" :value="opt.value">
                  {{ opt.label }}
                </option>
              </select>
              <input
                v-else-if="field.type === 'checkbox'"
                :id="`field-${field.name}`"
                v-model="values[field.name] as boolean"
                type="checkbox"
                :disabled="field.readonly"
                class="mt-1"
              />
              <p
                v-if="errors?.[field.name]?.length"
                class="mt-1 text-xs text-danger-text"
              >
                {{ errors[field.name].join(', ') }}
              </p>
            </div>
            </div>
          </template>
        </div>
      </template>
    </div>

    <!-- Save / Cancel -->
    <div class="flex justify-end gap-2 border-t border-border bg-bg px-4 py-2">
      <button
        type="button"
        data-test="form-cancel"
        class="btn btn-default px-8"
        :disabled="saving"
        @click="emit('cancel')"
      >
        {{ t('form.cancel') }}
      </button>
      <button type="submit" data-test="form-save" class="btn btn-success px-8" :disabled="saving">
        {{ t('form.save') }}
      </button>
    </div>
  </form>
</template>
