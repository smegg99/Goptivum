<!-- app/components/layout/PageHeader.vue -->

<script setup lang="ts">
defineProps<{
  title: string
  description?: string
}>()
</script>

<template>
  <header class="page-header">
    <div class="d-flex align-start ga-4 flex-wrap">
      <div class="flex-grow-1">
        <h1 class="text-h5 font-weight-bold ma-0">
          {{ title }}
        </h1>
        <p
          v-if="description"
          class="text-body-2 text-medium-emphasis mt-1 mb-0"
        >
          {{ description }}
        </p>
      </div>
      <slot name="actions" />
    </div>
  </header>
</template>
