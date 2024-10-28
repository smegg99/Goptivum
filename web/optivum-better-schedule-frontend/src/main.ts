/**
 * main.ts
 *
 * Bootstraps Vuetify and other plugins then mounts the App`
 */

// Plugins
import { registerPlugins } from '@/plugins'
import 'vuetify/styles';
import '@mdi/font/css/materialdesignicons.css';

// Components
import App from './App.vue'
import Overlay from './components/Overlay.vue';

// Composables
import { createApp, h } from 'vue'
import { createPinia } from 'pinia';
import { createI18n } from 'vue-i18n';
import router from './router'
import { useThemeStore } from './stores/themeStore';

// Locales
import en from './locales/en';
import pl from './locales/pl';
import uk from './locales/uk';
import { useTheme } from 'vuetify';

const i18n = createI18n({
	legacy: false,
	locale: localStorage.getItem('language') || 'en',
	fallbackLocale: 'en',
	messages: {
		en,
		pl,
		uk,
	},
});

const app = createApp({
	setup() {
		const theme = useTheme();
		const themeStore = useThemeStore(pinia);

		themeStore.applyTheme(theme);
		window
		.matchMedia('(prefers-color-scheme: dark)')
		.addEventListener('change', (event) => {
			themeStore.handleSystemThemeChange(theme, event);
		});
	},
	render: () => h(App),
});
registerPlugins(app)

const pinia = createPinia();

app.use(pinia);

const themeStore = useThemeStore(pinia);
themeStore.setTheme(themeStore.currentTheme);

app.use(router);
app.use(i18n);
app.mount('#app');
app.component('Overlay', Overlay);