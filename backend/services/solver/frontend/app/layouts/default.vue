<!-- app/layouts/default.vue -->

<template>
  <v-app>
    <LayoutAppBar />
    <LayoutAppNavigationDrawer />

    <v-main class="app-main">
      <div class="app-main__content">
        <slot />
      </div>
    </v-main>
  </v-app>
</template>
