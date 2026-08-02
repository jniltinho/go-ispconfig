<script setup lang="ts">
// Resellers list (limit_client != 0 rows): reuses the client DataTable
// against the admin-only /api/resellers surface.
import ClientList from './ClientList.vue'
</script>

<template>
  <ClientList
    api-base="/api/resellers"
    form-base="/clients/resellers"
    title-key="client.resellers_title"
    add-key="client.add_reseller"
  />
</template>
