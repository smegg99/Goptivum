<template>
	<div ref="tilt" class="tilt-wrapper">
		<v-btn class="button" :style="{ backgroundColor: getButtonColor(index) }" :ripple="true" elevation="8"
			variant="text" rounded="xl" :to="`/room/${props.id}`" nav link>
			<div class="button-content">
				<span :class="['full-name', fontSizeClass]">{{ full_name }}</span>
			</div>
		</v-btn>
	</div>
</template>

<script setup lang="ts">
import { ref, onUnmounted, watchEffect, computed } from 'vue';
import VanillaTilt from 'vanilla-tilt';
import { useTheme } from 'vuetify';
import { useMiscStore } from '@/stores/miscStore';

const theme = useTheme();
const miscStore = useMiscStore();
const reducedAnimationsEnabled = computed(() => miscStore.reducedAnimationsEnabled);

const getButtonColor = (index: number) => {
	const colors = theme.current.value.colors;
	return index % 2 === 0 ? colors.primaryMuted : colors.secondaryMuted;
};

interface VanillaTiltHTMLElement extends HTMLElement {
	vanillaTilt: VanillaTilt;
}

const props = defineProps<{
	text: string;
	designator: string;
	index: number;
	id: number;
}>();

const full_name = computed(() => {
	return props.text === props.designator ? props.text : `${props.text} (${props.designator})`;
});

const tilt = ref<VanillaTiltHTMLElement | null>(null);
const enableTilt = ref(window.innerWidth > 700 && !reducedAnimationsEnabled.value);

watchEffect(() => {
	if (enableTilt.value && tilt.value) {
		VanillaTilt.init(tilt.value, {
			max: 15,
			speed: 400,
			scale: 1.1,
			glare: false,
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

const fontSizeClass = computed(() => {
	if (props.text.length <= 5) return 'font-large';
	if (props.text.length <= 10) return 'font-medium';
	return 'font-small';
});
</script>

<style scoped>
.tilt-wrapper {
	display: inline-block;
	width: 100%;
	height: 100%;
	user-select: none;
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
}

.button-content {
	display: flex;
	flex-direction: column;
	justify-content: center;
	align-items: center;
	width: 100%;
	height: 100%;
	box-sizing: border-box;
}

.full-name {
	width: 90%;
	height: 90%;
	font-weight: 800;
	text-align: center;
	white-space: normal;
	display: flex;
	justify-content: center;
	align-items: center;
}

.font-large {
	font-size: 1.85rem;
	font-weight: 800;
}

.font-medium {
	font-size: 1.25rem;
	font-weight: 800;
}

.font-small {
	font-size: 0.9rem;
	font-weight: 800;
}

@media (max-width: 767px) {
	.button {
		padding: 0.8rem;
	}

	.button-content {
		padding: 0.4rem;
	}
}
</style>
