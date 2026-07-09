// app/plugins/vuetify.ts

import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import { defaults } from '~/theme/defaults'
import { light } from '~/theme/themes'

export default defineNuxtPlugin((nuxtApp) => {
  const vuetify = createVuetify({
    // createVuetify registers nothing by default; without these the whole
    // UI renders as unknown inert <v-*> elements.
    components,
    directives,
    defaults,
    theme: {
      defaultTheme: 'light',
      themes: { light },
    },
  })
  nuxtApp.vueApp.use(vuetify)
})
