<!-- Overlay.vue -->
<template>
  <v-navigation-drawer v-model="drawer" class="elevation-8">
    <template #prepend>
      <v-slide-x-transition appear>
        <v-list nav density="default">
          <v-list-item v-for="item in items" :key="item.title" :to="item.route" nav link
            :prepend-icon="item.prependIcon" :title="item.title" class="ma-1">
          </v-list-item>
        </v-list>
      </v-slide-x-transition>
    </template>

    <template #append>
      <v-slide-y-reverse-transition appear>
        <v-list nav density="default">
          <v-list-item class="ma-1" nav link prepend-icon="mdi-cog-outline" title="Settings" :to="'/settings'" />
        </v-list>
      </v-slide-y-reverse-transition>
    </template>
  </v-navigation-drawer>

  <v-card class="menu-card rounded-pill" elevation="8">
    <v-btn icon="mdi-menu" @click=" drawer=!drawer" />
  </v-card>

  <v-main>
    <div class="fill-height">
      <v-scroll-y-transition appear>
        <router-view class="fill-height fill-width" />
      </v-scroll-y-transition>
    </div>
  </v-main>
</template>

<script setup lang="ts">
import { ref } from 'vue'

const drawer = ref(true)

const items = ref([
  {
    title: 'Home',
    prependIcon: 'mdi-view-dashboard-outline',
    route: '/',
  },
])
</script>

<style scoped>
.no-scroll {
  overflow: hidden;
}

.grid-container {
  display: grid;
  grid-template-columns: 1fr 1fr;
  align-items: center;
  justify-items: center;
  max-height: 100%;
  overflow: hidden;
}

.menu-card {
  width: 32px;
  aspect-ratio: 1 / 1;
  display: inline-flex;
  padding: 32px;
  align-items: center;
  justify-content: center;
  position: fixed;
}

.v-btn {
  margin: 0;
  padding: 0;
}

.v-btn>.v-icon {
  font-size: 24px;
}
</style>
