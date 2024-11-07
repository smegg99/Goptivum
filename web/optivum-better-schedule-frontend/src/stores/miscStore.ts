// src/stores/miscStore.ts
import { defineStore } from 'pinia';
import { ref, watch } from 'vue';

export const useMiscStore = defineStore('misc', () => {
	const reducedAnimationsEnabled = ref(
		localStorage.getItem('reducedAnimationsEnabled') === 'true' || false
	);

	watch(reducedAnimationsEnabled, (newValue) => {
		localStorage.setItem('reducedAnimationsEnabled', String(newValue));
	});

	const toggleReducedAnimations = () => {
		reducedAnimationsEnabled.value = !reducedAnimationsEnabled.value;
	};

	return {
		reducedAnimationsEnabled,
		toggleReducedAnimations,
	};
});
