<!-- Overlay.vue -->
<template>
  <v-layout class="fill-height">
    <v-navigation-drawer v-model="drawer" class="elevation-8">
      <v-slide-x-transition appear>
        <v-list nav>
          <v-list-item v-for="item in items" :key="item.title" :to="item.route" nav link
            :prepend-icon="item.prependIcon" :title="item.title" class="ma-1">
          </v-list-item>
        </v-list>
      </v-slide-x-transition>

      <template #append>
        <v-slide-y-reverse-transition appear>
          <v-list nav>
            <v-list-item class="ma-1" nav link prepend-icon="mdi-cog-outline" title="Settings"
              :to="'/settings'" />
          </v-list>
        </v-slide-y-reverse-transition>
      </template>
    </v-navigation-drawer>

    <v-app-bar>
      <v-app-bar-nav-icon v-if="$vuetify.display.mdAndDown" @click="drawer = !drawer" />
      <v-slide-y-transition appear>
        <v-app-bar-title>Optivum Schedule Better Frontend</v-app-bar-title>
      </v-slide-y-transition>
    </v-app-bar>

    <v-main>
      <div class="fill-height pa-4">
        <v-scroll-y-transition appear>
          <router-view class="fill-height pa-8" />
        </v-scroll-y-transition>
      </div>
    </v-main>
  </v-layout>
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