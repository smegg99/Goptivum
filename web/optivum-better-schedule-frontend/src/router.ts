import { createRouter, createWebHistory } from 'vue-router'

import MainPage from './components/pages/Home.vue'
import SettingsPage from './components/pages/Settings.vue'

const routes = [
  { path: '/', component: MainPage },
  { path: '/home', component: MainPage },
  { path: '/settings', component: SettingsPage },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

export default router
