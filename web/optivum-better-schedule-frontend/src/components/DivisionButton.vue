<template>
	<div ref="tilt" class="tilt-wrapper">
		<v-btn class="square-button" :style="{ backgroundColor: getButtonColor(index) }" :ripple="true" elevation="8"
			variant="text" rounded="xl">
			<span>{{ props.text }}</span>
		</v-btn>
	</div>
</template>

<script setup lang="ts">
import { ref, onUnmounted, watchEffect } from 'vue';
import VanillaTilt from 'vanilla-tilt';
import { useTheme } from 'vuetify';

const theme = useTheme();

const getButtonColor = (index: number) => {
	const colors = theme.current.value.colors;
	return index % 2 === 0 ? colors.primaryMuted : colors.secondaryMuted;
};

interface VanillaTiltHTMLElement extends HTMLElement {
	vanillaTilt: VanillaTilt;
}

const props = defineProps<{ text: string; index: number }>();
const tilt = ref<VanillaTiltHTMLElement | null>(null);

// Determine if tilt should be enabled based on screen size
const enableTilt = ref(window.innerWidth > 700);


// Initialize or destroy VanillaTilt based on screen size
watchEffect(() => {
	if (enableTilt.value && tilt.value) {
		VanillaTilt.init(tilt.value, {
			max: 20,
			speed: 10,
			scale: 1.25,
			glare: false,
			reverse: false,
			transition: true,
		});
	} else if (tilt.value?.vanillaTilt) {
		tilt.value.vanillaTilt.destroy();
	}
});

// Listen for screen resize to enable or disable tilt
window.addEventListener('resize', () => {
	enableTilt.value = window.innerWidth > 700;
});

onUnmounted(() => {
	if (tilt.value?.vanillaTilt) {
		tilt.value.vanillaTilt.destroy();
	}
	window.removeEventListener('resize', () => {
		enableTilt.value = window.innerWidth > 700;
	});
});
</script>

<style scoped lang="scss">
/* Using Vuetify theme colors */
.tilt-wrapper {
	display: inline-block;
	width: 100%;
	height: 100%;
	user-select: none;
}

.square-button {
	z-index: 1;
	width: 100%;
	height: 100%;
	font-size: 2.5rem;
	font-weight: 400;
	display: flex;
	align-items: center;
	justify-content: center;
	user-select: none;
}

@media (max-width: 700px) {
	.square-button {
		font-size: 2rem;
	}
}
</style>
