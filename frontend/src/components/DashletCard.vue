<script setup lang="ts">
// Reusable dashlet card, the shape every legacy dashboard dashlet has:
// a bordered box with a title bar on top and the dashlet body below
// (themes/default/assets/stylesheets/ispconfig.sass `.table-wrapper` +
// `.table caption`). The bar carries the brand red so the cards read as a
// grid of boxes; the title is a real <h2> under the page <h1>, so the red
// is decoration only and nothing depends on colour alone.
defineProps<{ title: string }>()
</script>

<template>
  <section class="flex flex-col border border-border bg-dashlet">
    <h2 class="bg-brand px-2 py-1 text-xs font-bold tracking-wide text-white uppercase">
      {{ title }}
    </h2>
    <div class="flex-1 p-2">
      <slot />
    </div>
  </section>
</template>
