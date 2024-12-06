import { createRouter, createWebHistory, RouteLocationNormalized } from 'vue-router'
import MainPage from './components/pages/HomePage.vue'
import SettingsPage from './components/pages/SettingsPage.vue'

import ResourcePage from './components/pages/ResourcePage.vue'
import ScheduleTable from './components/outputs/ScheduleTable.vue'
import DutiesTable from './components/outputs/DutiesTable.vue'

const routes = [
  { path: '/', component: MainPage, meta: { titleKey: 'page.home' } },
  { path: '/home', component: MainPage, meta: { titleKey: 'page.home' } },
  { path: '/settings', component: SettingsPage, meta: { titleKey: 'page.settings' } },
  { 
    path: '/divisions', 
    component: ResourcePage, 
    props: () => ({ type: 'division' }),
    meta: { titleKey: 'page.divisions' } 
  },
  { 
    path: '/teachers', 
    component: ResourcePage, 
    props: () => ({ type: 'teacher' }), 
    meta: { titleKey: 'page.teachers' } 
  },
  { 
    path: '/rooms', 
    component: ResourcePage, 
    props: () => ({ type: 'room' }),
    meta: { titleKey: 'page.rooms' } 
  },
  { 
    path: '/map', 
    component: MainPage, 
    props: (route: RouteLocationNormalized) => ({ id: route.params.id as string }), 
    meta: { titleKey: 'page.map' } 
  },
  {
    path: '/duties',
    component: ScheduleTable, 
    props: (route: RouteLocationNormalized) => ({ id: route.params.id as string, type: 'duty' }), 
    meta: { titleKey: 'page.duties' } 
  },
  {
    path: '/practices',
    component: MainPage, 
    props: (route: RouteLocationNormalized) => ({ id: route.params.id as string, type: 'practices' }),
    meta: { titleKey: 'page.practices' } 
  },
  { 
    path: '/division/:id', 
    component: ScheduleTable, 
    props: (route: RouteLocationNormalized) => ({ id: route.params.id as string, type: 'division' }), 
    meta: { titleKey: 'page.division' } 
  },
  { 
    path: '/teacher/:id', 
    component: ScheduleTable, 
    props: (route: RouteLocationNormalized) => ({ id: route.params.id as string, type: 'teacher' }), 
    meta: { titleKey: 'page.teacher' } 
  },
  { 
    path: '/room/:id', 
    component: ScheduleTable, 
    props: (route: RouteLocationNormalized) => ({ id: route.params.id as string, type: 'room' }), 
    meta: { titleKey: 'page.room' } 
  },
];

const router = createRouter({
  history: createWebHistory(),
  routes,
})

export default router
