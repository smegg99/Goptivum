<template>
	<v-container class="d-flex justify-center align-center">
		<div class="text-center">
			<span class="language-title mb-6">{{ t('language.name') }}</span>
			<v-select
				v-model="currentLanguage"
				:items="languages"
				item-title="label"
				item-value="code"
				:label="t('language.select')"
				:menu-props="{ attach: 'body', locationStrategy: 'connected' }"
				class="language-select elevation-8"
				color="tertiary"
				variant="outlined"
			></v-select>
		</div>
	</v-container>
</template>

<script setup>
import { ref, watch, onMounted, computed } from 'vue';
import { useI18n } from 'vue-i18n';
import ISO6391 from 'iso-639-1';

const { locale, t } = useI18n();

const currentLanguage = ref(locale.value || 'en');
const languageCodes = ['en', 'pl', 'uk'];

const languages = computed(() => {
	return languageCodes.map((code) => {
		let label = ISO6391.getNativeName(code);
		return { code, label };
	});
});

onMounted(() => {
	const savedLanguage = localStorage.getItem('language');
	if (savedLanguage) {
		currentLanguage.value = savedLanguage;
		locale.value = savedLanguage;
	}
});

watch(currentLanguage, (newLanguage) => {
	localStorage.setItem('language', newLanguage);
	locale.value = newLanguage;
});
</script>

<style scoped>
.language-title {
	font-size: 1.25rem;
	font-weight: bold;
	display: block;
	user-select: none;
}

:deep(.v-input__details) {
	display: none;
}
</style>
