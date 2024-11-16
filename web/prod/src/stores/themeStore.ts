// src/stores/themeStore.ts
import { defineStore } from 'pinia';
import { ref } from 'vue';

export const useThemeStore = defineStore('theme', () => {
	const currentTheme = ref(localStorage.getItem('theme') || 'auto');
	const setTheme = (themeValue: string) => {
		currentTheme.value = themeValue;
		localStorage.setItem('theme', themeValue);
	};

	const applyTheme = (themeInstance: any) => {
		if (currentTheme.value === 'auto') {
		const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
		themeInstance.global.name.value = prefersDark ? 'dracula' : 'light';
		} else {
		themeInstance.global.name.value = currentTheme.value;
		}
	};

	const handleSystemThemeChange = (themeInstance: any, event: MediaQueryListEvent) => {
		if (currentTheme.value === 'auto') {
		themeInstance.global.name.value = event.matches ? 'dracula' : 'light';
		}
	};

	return {
		currentTheme,
		setTheme,
		applyTheme,
		handleSystemThemeChange,
	};
});
