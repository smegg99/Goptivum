import { createRouter, createWebHistory } from 'vue-router'
import MainPage from './components/pages/Home.vue'
import DivisionsPage from './components/pages/Divisions.vue'
import SettingsPage from './components/pages/Settings.vue'

import DivisionPage from './components/pages/Division.vue'

const routes = [
  { path: '/', component: MainPage, meta: { titleKey: 'page.home' } },
  { path: '/home', component: MainPage, meta: { titleKey: 'page.home' } },
  { path: '/divisions', component: DivisionsPage, meta: { titleKey: 'page.divisions' } },
  { path: '/teachers', component: MainPage, meta: { titleKey: 'page.teachers' } },
  { path: '/rooms', component: MainPage, meta: { titleKey: 'page.rooms' } },
  { path: '/settings', component: SettingsPage, meta: { titleKey: 'page.settings' } },

  { path: '/division/:id', component: DivisionPage, props: true, meta: { titleKey: 'page.division' } },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

export default router
