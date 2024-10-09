<template>
	<v-container class="fill-height d-flex justify-center align-center">
		<div class="text-center">
			<span class="theme-title">Theme</span>
			<v-btn-toggle v-model="currentTheme" mandatory class="ma-4 elevation-8" color="tertiary" variant="outlined">
				<v-btn value="dark">
					<span>Dark</span>
					<v-icon end>mdi-weather-night</v-icon>
				</v-btn>
				<v-btn value="auto">
					<span>Auto</span>
					<v-icon end>mdi-auto-mode</v-icon>
				</v-btn>
				<v-btn value="light">
					<span>Light</span>
					<v-icon end>mdi-weather-sunny</v-icon>
				</v-btn>
			</v-btn-toggle>
		</div>
	</v-container>
</template>

<script setup>
import { ref, watch, onMounted, onUnmounted } from 'vue';
import { useTheme } from 'vuetify';

const currentTheme = ref('auto');
const theme = useTheme();

onMounted(() => {
	const savedTheme = localStorage.getItem('theme');
	if (savedTheme) {
		currentTheme.value = savedTheme;
		applyTheme(savedTheme);
	} else {
		applyAutoTheme();
	}

	window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', handleSystemThemeChange);
});

watch(currentTheme, (newTheme) => {
	localStorage.setItem('theme', newTheme);
	applyTheme(newTheme);
});

function applyTheme(themeValue) {
	if (themeValue === 'auto') {
		applyAutoTheme();
	} else {
		theme.global.name.value = themeValue;
	}
}

function applyAutoTheme() {
	const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
	theme.global.name.value = prefersDark ? 'dark' : 'light';
}

function handleSystemThemeChange(event) {
	if (currentTheme.value === 'auto') {
		theme.global.name.value = event.matches ? 'dark' : 'light';
	}
}

onUnmounted(() => {
	window.matchMedia('(prefers-color-scheme: dark)').removeEventListener('change', handleSystemThemeChange);
});
</script>

<style scoped>
.theme-title {
	font-size: 1.25rem;
	font-weight: bold;
	display: block;
	user-select: none;
}
</style>
