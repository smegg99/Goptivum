export default {
  // Global page headers: https://go.nuxtjs.dev/config-head
  head: {
    title: 'schedule-frontend-mock',
    htmlAttrs: {
      lang: 'en'
    },
    meta: [
      { charset: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { hid: 'description', name: 'description', content: '' },
      { name: 'format-detection', content: 'telephone=no' }
    ],
    link: [
      { rel: 'icon', type: 'image/x-icon', href: '/favicon.ico' }
    ]
  },

  // Global CSS: https://go.nuxtjs.dev/config-css
  css: [
  ],

  // Plugins to run before rendering page: https://go.nuxtjs.dev/config-plugins
  plugins: [
  ],

  // Auto import components: https://go.nuxtjs.dev/config-components
  components: true,

  // Modules for dev and build (recommended): https://go.nuxtjs.dev/config-modules
  buildModules: [
  ],

  // Modules: https://go.nuxtjs.dev/config-modules
  modules: [
  ],

  // Build Configuration: https://go.nuxtjs.dev/config-build
  build: {
  },

  router: {
    extendRoutes (routes, resolve) {
      // Clear the existing routes
      routes.splice(0)

      // Define custom routes
      routes.push(
        {
          path: '/',
          component: resolve(__dirname, 'pages/index.vue'),
          name: 'home'
        },
        {
          path: '/n1.html',
          component: resolve(__dirname, 'pages/TeachersPage.vue'),
          name: 'TeachersPage'
        },
        {
          path: '/nnn.php',
          component: resolve(__dirname, 'pages/TeachersList.vue'),
          name: 'TeachersList'
        },
        {
          path: '/o1.html',
          component: resolve(__dirname, 'pages/DivisionsPage.vue'),
          name: 'DivisionsPage'
        },
        {
          path: '/lll.php',
          component: resolve(__dirname, 'pages/DivisionsList.vue'),
          name: 'DivisionsList'
        },
        {
          path: '/s1.html',
          component: resolve(__dirname, 'pages/RoomsPage.vue'),
          name: 'RoomsPage'
        },
        {
          path: '/sss.php',
          component: resolve(__dirname, 'pages/RoomsList.vue'),
          name: 'RoomsList'
        }
      )
    }
  }
}
