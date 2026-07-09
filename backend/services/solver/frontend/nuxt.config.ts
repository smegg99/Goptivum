// nuxt.config.ts

const backend = process.env.ARRANGO_BACKEND ?? 'http://127.0.0.1:18080'

export default defineNuxtConfig({
  compatibilityDate: '2025-07-01',
  ssr: false,
  devtools: { enabled: false },
  css: [
    'vuetify/styles',
    '@mdi/font/css/materialdesignicons.css',
    '~/assets/css/layout.css',
  ],
  build: { transpile: ['vuetify'] },
  vite: {
    vue: { template: { transformAssetUrls: { 'v-img': ['src'] } } },
  },
  nitro: {
    devProxy: {
      '/api': { target: `${backend}/api`, changeOrigin: true },
    },
  },
})
