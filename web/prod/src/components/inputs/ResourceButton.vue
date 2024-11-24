<template>
	<div ref="tilt" class="tilt-wrapper">
		<v-btn
			:class="['button', getButtonClass(index), buttonSizeClass]"
			:ripple="true"
			elevation="8"
			variant="text"
			rounded="xl"
			:to="getLink"
			nav
			link
			@dragstart.prevent
			@mousedown.stop
			draggable="false"
		>
			<div class="button-content">
				<span :class="[fontSizeClass, 'content']">{{ displayText }}</span>
			</div>
		</v-btn>
	</div>
</template>

<script setup lang="ts">
import { ref, onUnmounted, watchEffect, computed } from 'vue';
import VanillaTilt from 'vanilla-tilt';
import { useMiscStore } from '@/stores/miscStore';
import { useBackgroundGradientClass } from '@/composables/useThemeStyles';
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
const reducedAnimationsEnabled = computed(() => miscStore.reducedAnimationsEnabled);

const getLink = computed(() => {
	return `/${props.type}/${props.id}`;
});

const getButtonClass = (index: number) => {
	return index % 2 === 0 ? bkGradPrimary.value : bkGradSecondary.value;
};

const displayText = computed(() => {
	if (props.type === 'teacher') return props.text;
	return props.designator;
});

const buttonSizeClass = computed(() => {
	return props.type === 'teacher' ? 'size-teacher' : 'size-default';
});

const { fontSizeClass } = useFontSizeClass(displayText);

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
	enableTilt.value = window.innerWidth > 700 && !reducedAnimationsEnabled.value;
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
	display: flex;
	align-items: center;
	justify-content: center;
	text-align: center;
	overflow: hidden;
	box-sizing: border-box;
	border-radius: 24px !important;
	-webkit-user-drag: none;
	pointer-events: auto;
	position: relative;
}

.size-default {
	width: 100%;
	height: 0;
	padding-bottom: 100%;
}

.size-teacher {
	width: 200%;
	height: 0;
	padding-bottom: 50%;

	@media (max-width: 485px) {
		width: 100%;
	}
}

.button-content {
	position: absolute;
	top: 50%;
	left: 50%;
	transform: translate(-50%, -50%);
	width: 90%;
	height: 90%;
	display: flex;
	flex-direction: column;
	justify-content: center;
	align-items: center;
	box-sizing: border-box;
	overflow: visible;
}

.content {
	font-weight: 900;
	text-align: center;
	white-space: nowrap;
	display: block;
	justify-content: center;
	align-items: center;
	word-wrap: normal;
	overflow: hidden;
	text-overflow: ellipsis;
	width: 100%;
}
</style>