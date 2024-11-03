import { createRouter, createWebHistory } from 'vue-router'
import MainPage from './components/pages/Home.vue'
import DivisionsPage from './components/pages/Divisions.vue'
import TeachersPage from './components/pages/Teachers.vue'
import RoomsPage from './components/pages/Rooms.vue'
import SettingsPage from './components/pages/Settings.vue'

import DivisionPage from './components/pages/Division.vue'
import TeacherPage from './components/pages/Teacher.vue'
import RoomPage from './components/pages/Room.vue'

const routes = [
  { path: '/', component: MainPage, meta: { titleKey: 'page.home' } },
  { path: '/home', component: MainPage, meta: { titleKey: 'page.home' } },
  { path: '/divisions', component: DivisionsPage, meta: { titleKey: 'page.divisions' } },
  { path: '/teachers', component: TeachersPage, meta: { titleKey: 'page.teachers' } },
  { path: '/rooms', component: RoomsPage, meta: { titleKey: 'page.rooms' } },
  { path: '/settings', component: SettingsPage, meta: { titleKey: 'page.settings' } },

  { path: '/division/:id', component: DivisionPage, props: true, meta: { titleKey: 'page.division' } },
  { path: '/teacher/:id', component: TeacherPage, props: true, meta: { titleKey: 'page.teacher' } },
  { path: '/room/:id', component: RoomPage, props: true, meta: { titleKey: 'page.room' } },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

export default router
