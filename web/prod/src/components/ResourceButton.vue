<!-- GenericButton.vue -->
<template>
	<div ref="tilt" class="tilt-wrapper">
		<v-btn :class="['button', getButtonClass(index)]" :ripple="true" elevation="8" variant="text" rounded="xl"
			:to="getLink" nav link>
			<div class="button-content">
				<span :class="['full-name', textGradPrimaryAccent, fontSizeClass]">{{ fullName }}</span>
				<span v-if="showDesignator" :class="textGradSecondaryAccent" class="designator">{{ props.designator
					}}</span>
			</div>
		</v-btn>
	</div>
</template>

<script setup lang="ts">
import { ref, onUnmounted, watchEffect, computed } from 'vue';
import VanillaTilt from 'vanilla-tilt';
import { useMiscStore } from '@/stores/miscStore';
import { useBackgroundGradientClass, useTextGradientClass } from '@/composables/useThemeStyles';
import { useFontSizeClass } from '@/composables/useFontSizeClass';

const props = defineProps<{
	type: 'division' | 'teacher' | 'room';
	text: string;
	designator: string;
	index: number;
	id: number;
}>();

const miscStore = useMiscStore();
const bkGradPrimary = useBackgroundGradientClass('primary');
const bkGradSecondary = useBackgroundGradientClass('secondary');
const textGradPrimaryAccent = useTextGradientClass('primary-accent');
const textGradSecondaryAccent = useTextGradientClass('secondary-accent');
const { fontSizeClass } = useFontSizeClass(props.text)
const reducedAnimationsEnabled = computed(() => miscStore.reducedAnimationsEnabled);

const getLink = computed(() => {
	return `/${props.type}/${props.id}`;
});

const showDesignator = computed(() => {
	return props.text !== props.designator;
});

const getButtonClass = (index: number) => {
	return index % 2 === 0 ? bkGradPrimary.value : bkGradSecondary.value;
};

const fullName = computed(() => {
	return props.text;
});

interface VanillaTiltHTMLElement extends HTMLElement {
	vanillaTilt: VanillaTilt;
}

const tilt = ref<VanillaTiltHTMLElement | null>(null);
const enableTilt = ref(window.innerWidth > 700 && !reducedAnimationsEnabled.value);

watchEffect(() => {
	if (enableTilt.value && tilt.value) {
		VanillaTilt.init(tilt.value, {
			max: 15,
			speed: 400,
			scale: 1.1,
			glare: true,
			reverse: false,
			transition: true,
		});
	} else if (tilt.value?.vanillaTilt) {
		tilt.value.vanillaTilt.destroy();
	}
});

const resizeHandler = () => {
	enableTilt.value = window.innerWidth > 700;
};

window.addEventListener('resize', resizeHandler);

onUnmounted(() => {
	if (tilt.value?.vanillaTilt) {
		tilt.value.vanillaTilt.destroy();
	}
	window.removeEventListener('resize', resizeHandler);
});
</script>

<style scoped lang="scss">
.tilt-wrapper {
	display: inline-block;
	width: 100%;
	height: 100%;
	user-select: none;
	border-radius: 24px !important;
	overflow: visible !important;
}

.button {
	width: 100%;
	height: 100%;
	display: flex;
	align-items: center;
	justify-content: center;
	text-align: center;
	overflow: hidden;
	padding: 1rem;
	box-sizing: border-box;
	border-radius: 24px !important;
}

.button-content {
	display: flex;
	flex-direction: column;
	justify-content: center;
	align-items: center;
	width: 100%;
	height: 100%;
	box-sizing: border-box;
	overflow: visible
}

.full-name {
	font-weight: 800;
	text-align: center;
	white-space: normal;
	display: flex;
	justify-content: center;
	align-items: center;
}

.designator {
	font-size: clamp(0.85rem, 1vw + 0.3rem, 1rem);
	letter-spacing: 0.15rem;
	font-weight: 600;
	text-align: center;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}

.js-tilt-glare {
	border-radius: 24px !important;
}

@media (max-width: 767px) {
	.button {
		padding: 0.8rem;
	}

	.button-content {
		padding: 0.4rem;
	}

	.designator {
		font-size: 1rem;
	}
}
</style>
