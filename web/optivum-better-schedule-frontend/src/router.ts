import { createRouter, createWebHistory } from 'vue-router'

import MainPage from './components/pages/Home.vue'
import DivisionsPage from './components/pages/Divisions.vue'
import SettingsPage from './components/pages/Settings.vue'

const routes = [
  { path: '/', component: MainPage },
  { path: '/home', component: MainPage },
  { path: '/divisions', component: DivisionsPage },
  { path: '/settings', component: SettingsPage },

  { path: '/division/:id', component: DivisionsPage, props: true },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

export default router
