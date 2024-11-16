<!-- LanguageSwitcher.vue -->
<template>
	<v-container class="d-flex justify-center align-center pa-0">
		<div class="text-center">
			<span class="language-title mb-6">{{ t('language.name') }}</span>
			<v-select v-model="currentLanguage" :items="languages" item-title="label" item-value="code"
				:label="t('language.select')" :menu-props="{ attach: 'body', locationStrategy: 'connected' }"
				class="language-select elevation-8" color="tertiary" variant="outlined"></v-select>
		</div>
	</v-container>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import ISO6391 from 'iso-639-1';
import { useLocaleStore } from '../stores/localeStore';

const { t } = useI18n();
const localeStore = useLocaleStore();

const currentLanguage = ref(localeStore.currentLocale);
const languageCodes = ['pl', 'en', 'uk'];

const languages = computed(() => {
	return languageCodes.map((code) => {
		let label = ISO6391.getNativeName(code);
		return { code, label };
	});
});

watch(
	() => currentLanguage.value,
	(newLanguage) => {
		localeStore.setLocale(newLanguage);
	}
);

watch(
	() => localeStore.currentLocale,
	(newLocale) => {
		currentLanguage.value = newLocale;
	}
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
