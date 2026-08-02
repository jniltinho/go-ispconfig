<script setup lang="ts">
// ISPConfig-style tabbed form: flat tabs on top of a white card, fields
// rendered from JSON form metadata, labels on the left with an automatic ':',
// green Save / Cancel buttons and per-field errors in alert-danger style.
import { reactive, ref, watch } from 'vue'
import { useI18n } from '../i18n'

export interface FormField {
  name: string
  type: 'text' | 'password' | 'textarea' | 'select' | 'checkbox'
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
}>()

const emit = defineEmits<{
  (e: 'save', values: Record<string, unknown>): void
  (e: 'cancel'): void
}>()

const { t } = useI18n()
const activeTab = ref(props.metadata.tabs[0]?.name ?? '')

// Working copy of the values: modelValue > field default.
const values = reactive<Record<string, unknown>>({})
for (const tab of props.metadata.tabs) {
  for (const field of tab.fields) {
    values[field.name] =
      props.modelValue?.[field.name] ??
      field.default ??
      (field.type === 'checkbox' ? false : '')
  }
}

const errorList = () => Object.entries(props.errors ?? {}).filter(([, msgs]) => msgs.length > 0)

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
  <form class="border border-border bg-surface" @submit.prevent="emit('save', { ...values })">
    <!-- Flat tabs -->
    <div class="flex border-b border-border bg-bg">
      <button
        v-for="tab in metadata.tabs"
        :key="tab.name"
        type="button"
        class="border-r border-border px-5 py-2.5 text-sm font-bold"
        :class="activeTab === tab.name ? 'bg-surface text-text' : 'text-text/70 hover:bg-info'"
        @click="activeTab = tab.name"
      >
        {{ tab.label }}
      </button>
    </div>

    <!-- Error summary (alert-danger style) -->
    <div
      v-if="errorList().length"
      class="m-4 flex border border-danger-border bg-danger text-sm text-danger-text"
    >
      <span class="w-[60px] shrink-0 self-stretch bg-danger-border/50 px-2 py-2 font-bold">
        {{ t('form.error_label') }}
      </span>
      <ul class="list-disc py-2 pl-8 pr-3">
        <li v-for="[field, msgs] in errorList()" :key="field">{{ msgs.join(', ') }}</li>
      </ul>
    </div>

    <!-- Fields of the active tab -->
    <div class="px-3 py-6">
      <template v-for="tab in metadata.tabs" :key="tab.name">
        <div v-show="activeTab === tab.name" class="space-y-4">
          <div v-for="field in tab.fields" :key="field.name" class="flex items-start gap-4">
            <label
              :for="`field-${field.name}`"
              class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']"
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
                class="w-full max-w-md border border-border bg-surface px-3 py-1.5 text-sm outline-none focus:border-link disabled:bg-bg disabled:text-text/60"
                :class="{ 'border-danger-border': errors?.[field.name]?.length }"
              />
              <textarea
                v-else-if="field.type === 'textarea'"
                :id="`field-${field.name}`"
                v-model="values[field.name] as string"
                rows="4"
                :disabled="field.readonly"
                class="w-full max-w-md border border-border bg-surface px-3 py-1.5 text-sm outline-none focus:border-link disabled:bg-bg disabled:text-text/60"
                :class="{ 'border-danger-border': errors?.[field.name]?.length }"
              />
              <select
                v-else-if="field.type === 'select'"
                :id="`field-${field.name}`"
                v-model="values[field.name] as string"
                class="w-full max-w-md border border-border bg-surface px-3 py-1.5 text-sm outline-none focus:border-link"
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
                class="mt-2"
              />
              <p
                v-if="errors?.[field.name]?.length"
                class="mt-1 text-xs text-danger-text"
              >
                {{ errors[field.name].join(', ') }}
              </p>
            </div>
          </div>
        </div>
      </template>
    </div>

    <!-- Save / Cancel -->
    <div class="flex justify-end gap-2 border-t border-border bg-bg px-4 py-3">
      <button
        type="button"
        class="btn btn-default px-8"
        @click="emit('cancel')"
      >
        {{ t('form.cancel') }}
      </button>
      <button
        type="submit"
        class="btn btn-success px-8"
      >
        {{ t('form.save') }}
      </button>
    </div>
  </form>
</template>
