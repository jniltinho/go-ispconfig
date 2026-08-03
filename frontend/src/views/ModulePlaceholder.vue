<script setup lang="ts">
// Placeholder content for modules whose changes land later.
import { useI18n } from '../i18n'

const { t } = useI18n()

// Optional heading so a stub page still says which screen it stands in for.
defineProps<{ titleKey?: string }>()
</script>

<template>
  <div>
    <h1 v-if="titleKey" class="page-title">{{ t(titleKey) }}</h1>
    <div class="border border-border bg-surface p-6 text-sm">
      {{ t('placeholder.coming_soon') }}
    </div>
  </div>
</template>
