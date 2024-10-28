// src/stores/localeStore.ts
import { defineStore } from 'pinia';
import { ref, watch } from 'vue';
import { useI18n } from 'vue-i18n';

export const useLocaleStore = defineStore('locale', () => {
const { locale } = useI18n();
const currentLocale = ref(localStorage.getItem('language') || 'en');

watch(currentLocale, (newLocale) => {
	locale.value = newLocale;
	localStorage.setItem('language', newLocale);
});

watch(
	() => locale.value,
	(newLocale) => {
	currentLocale.value = newLocale;
	localStorage.setItem('language', newLocale);
	}
);

return {
	currentLocale,
	setLocale(newLocale: string) {
	currentLocale.value = newLocale;
	},
};
});
