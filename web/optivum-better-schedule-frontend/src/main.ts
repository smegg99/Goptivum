/**
 * main.ts
 *
 * Bootstraps Vuetify and other plugins then mounts the App`
 */

// Plugins
import { registerPlugins } from '@/plugins'
import 'vuetify/styles';
import vuetify from './plugins/vuetify';
import '@mdi/font/css/materialdesignicons.css';

// Components
import App from './App.vue'
import Overlay from './components/Overlay.vue';

// Composables
import { createApp } from 'vue'
import { createI18n } from 'vue-i18n';
import router from './router'

// Locales
import en from './locales/en';
import pl from './locales/pl';
import uk from './locales/uk';

const i18n = createI18n({
	legacy: false,
	locale: 'en',
	fallbackLocale: 'en',
	messages: {
		en,
		pl,
		uk,
	},
});

const app = createApp(App)
registerPlugins(app)

app.use(vuetify);
app.use(router);
app.use(i18n);
app.mount('#app');
app.component('Overlay', Overlay);