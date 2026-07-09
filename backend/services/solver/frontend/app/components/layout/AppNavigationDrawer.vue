<!-- app/components/layout/AppNavigationDrawer.vue -->

<script setup lang="ts">
const rail = ref(true)
</script>

<template>
  <v-navigation-drawer
    v-model:rail="rail"
    permanent
    width="232"
    rail-width="64"
    @mouseenter="rail = false"
    @mouseleave="rail = true"
  >
    <LayoutAppDrawerNavList />
  </v-navigation-drawer>
</template>
