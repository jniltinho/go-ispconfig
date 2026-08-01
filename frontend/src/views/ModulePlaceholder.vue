<script setup lang="ts">
// Placeholder content for modules whose changes land later.
import { useI18n } from '../i18n'

const { t } = useI18n()
</script>

<template>
  <div class="border border-border bg-surface p-6 text-sm">
    {{ t('placeholder.coming_soon') }}
  </div>
</template>
