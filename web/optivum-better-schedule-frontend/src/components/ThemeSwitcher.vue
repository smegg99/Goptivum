<!-- ThemeSwitcher.vue -->
<template>
	<v-container class="fill-height d-flex justify-center align-center">
		<div class="text-center">
			<span class="theme-title">{{ t('theme.name') }}</span>
			<v-btn-toggle v-model="currentTheme" mandatory class="ma-4 elevation-8" color="tertiary" variant="outlined">
				<v-btn value="dracula">
					<span>{{ t('theme.options.dracula') }}</span>
					<v-icon end>mdi-ghost</v-icon>
				</v-btn>
				<v-btn value="dark">
					<span>{{ t('theme.options.dark') }}</span>
					<v-icon end>mdi-weather-night</v-icon>
				</v-btn>
				<v-btn value="auto">
					<span>{{ t('theme.options.auto') }}</span>
					<v-icon end>mdi-auto-mode</v-icon>
				</v-btn>
				<v-btn value="light">
					<span>{{ t('theme.options.light') }}</span>
					<v-icon end>mdi-weather-sunny</v-icon>
				</v-btn>
			</v-btn-toggle>
		</div>
	</v-container>
</template>

<script setup lang="ts">
import { watch, computed } from 'vue';
import { useI18n } from 'vue-i18n';
import { useThemeStore } from '@/stores/themeStore';
import { useTheme } from 'vuetify';

const { t } = useI18n();
const themeStore = useThemeStore();
const themeInstance = useTheme();

const currentTheme = computed({
	get: () => themeStore.currentTheme,
	set: (value) => themeStore.setTheme(value),
});

watch(
	() => currentTheme.value,
	() => {
		themeStore.applyTheme(themeInstance);
	},
	{ immediate: true }
);
</script>

<style scoped>
.theme-title {
	font-size: 1.25rem;
	font-weight: bold;
	display: block;
	user-select: none;
}
</style>
