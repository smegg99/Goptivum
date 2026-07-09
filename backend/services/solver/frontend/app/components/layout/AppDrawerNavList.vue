<!-- app/components/layout/AppDrawerNavList.vue -->

<script setup lang="ts">
const navItems = [
  { to: '/', title: 'Solve', icon: 'mdi-cog-play-outline' },
  { to: '/data', title: 'Data', icon: 'mdi-database-edit-outline' },
  { to: '/schedule', title: 'Schedule', icon: 'mdi-calendar-month-outline' },
  { to: '/conflicts', title: 'Conflicts', icon: 'mdi-alert-circle-outline' },
  { to: '/scores', title: 'Scores', icon: 'mdi-chart-box-outline' },
]
</script>

<template>
  <v-list
    nav
    density="compact"
    class="pa-2"
  >
    <v-list-item
      v-for="item in navItems"
      :key="item.to"
      :to="item.to"
      :title="item.title"
      :prepend-icon="item.icon"
    />
  </v-list>
</template>
