<!-- TeacherButton.vue -->
<template>
	<div ref="tilt" class="tilt-wrapper">
		<v-btn class="button" :style="{ backgroundColor: getButtonColor(index) }" :ripple="true" elevation="8"
			variant="text" rounded="xl" :to="`/teacher/${props.id}`" nav link>
			<div class="button-content">
				<span class="full-name">{{ props.text }}</span>
				<span class="designator">{{ props.designator }}</span>
			</div>
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

const props = defineProps<{
	text: string;
	designator: string;
	index: number;
	id: number;
}>();

const tilt = ref<VanillaTiltHTMLElement | null>(null);
const enableTilt = ref(window.innerWidth > 700);

watchEffect(() => {
	if (enableTilt.value && tilt.value) {
		VanillaTilt.init(tilt.value, {
			max: 15,
			speed: 400,
			scale: 1.05,
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
	padding: 0.5rem;
	box-sizing: border-box;
}

.full-name {
	font-size: clamp(0.5rem, 1vw + 0.4rem, 0.8rem);
	font-weight: 600;
	text-align: center;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	margin-bottom: 0.3rem;
}

.designator {
	font-size: clamp(0.8rem, 1vw + 0.3rem, 1rem);
	font-weight: 800;
	text-align: center;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}

@media (max-width: 767px) {
	.button {
		padding: 0.8rem;
	}

	.button-content {
		padding: 0.4rem;
	}

	.full-name {
		font-size: 0.9rem;
		font-weight: 800;
		white-space: normal;
	}

	.designator {
		font-size: 0.9rem;
		font-weight: 600;
	}
}
</style>
