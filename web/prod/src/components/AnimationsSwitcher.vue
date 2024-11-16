<!-- AnimationsSwitcher.vue -->
<template>
	<v-container class="fill-height d-flex justify-center align-center pa-0">
		<div class="text-center">
			<span class="animations-title">{{ t('animations.name') }}</span>
			<v-switch v-model="reducedAnimationsEnabled" hide-details inset color="secondary" />
		</div>
	</v-container>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { useI18n } from 'vue-i18n';
import { useMiscStore } from '@/stores/miscStore';

const { t } = useI18n();

const miscStore = useMiscStore();
const reducedAnimationsEnabled = computed({
	get: () => miscStore.reducedAnimationsEnabled,
	set: (value) => {
		miscStore.reducedAnimationsEnabled = value;
	},
});
</script>

<style scoped>
.text-center {
	display: flex;
	flex-direction: column;
	justify-content: center;
	align-items: center;
	text-align: center;
}

.animations-title {
	font-size: 1.25rem;
	font-weight: bold;
	display: block;
	user-select: none;
	margin-top: 1rem;
}
</style>
