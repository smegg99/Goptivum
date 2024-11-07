<!-- ThemeSwitcher.vue -->
<template>
	<v-container class="fill-height d-flex justify-center align-center pa-0">
		<div class="text-center">
			<span class="theme-title">{{ t('theme.name') }}</span>
			<v-btn-toggle v-model="currentTheme" mandatory class="ma-4 elevation-8" color="tertiary" variant="outlined">
				<v-btn value="dracula" icon="mdi-ghost" />
				<v-btn value="dark" icon="mdi-weather-night" />
				<v-btn value="auto" icon="mdi-auto-mode" />
				<v-btn value="light" icon="mdi-weather-sunny" />
				<v-btn value="oled" icon="mdi-sprout" />
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
.text-center {
	display: flex;
	flex-direction: column;
	justify-content: center;
	align-items: center;
	text-align: center;
}

.theme-title {
	font-size: 1.25rem;
	font-weight: bold;
	display: block;
	user-select: none;
}
</style>
