<!-- Clock.vue -->
<template>
	<v-card class="clock-card d-flex flex-nowrap" flat>
		<v-col v-for="(unit, index) in timeUnits" :key="index" class="d-flex align-center pa-0" cols="auto">
			<div class="digit-group">
				<DigitalClockDigit v-for="(digit, digitIndex) in unit.digits" :key="digitIndex" :value="digit" />
				<span v-if="index < timeUnits.length - 1" :class="[textGradPrimaryAccent, 'colon']">:</span>
			</div>
		</v-col>
	</v-card>
</template>

<script lang="ts" setup>
import { ref, computed, onMounted, onUnmounted } from 'vue';
import { useTextGradientClass } from '@/composables/useThemeStyles';
import DigitalClockDigit from '@/components/outputs/Digit.vue';

const textGradPrimaryAccent = useTextGradientClass('primary-accent');

const time = ref(new Date());
let updateInterval: ReturnType<typeof setInterval> | null = null;

const startClock = () => {
	updateInterval = setInterval(() => {
		time.value = new Date();
	}, 1000);
};

const stopClock = () => {
	if (updateInterval) {
		clearInterval(updateInterval);
		updateInterval = null;
	}
};

const handleVisibilityChange = () => {
	if (document.hidden) {
		stopClock();
	} else {
		time.value = new Date();
		startClock();
	}
};

onMounted(() => {
	startClock();
	document.addEventListener('visibilitychange', handleVisibilityChange);
});

onUnmounted(() => {
	stopClock();
	document.removeEventListener('visibilitychange', handleVisibilityChange);
});

const getSeconds = computed(() => time.value.getSeconds().toString().padStart(2, '0'));
const getMinutes = computed(() => time.value.getMinutes().toString().padStart(2, '0'));
const getHours = computed(() => time.value.getHours().toString().padStart(2, '0'));

const splitDigits = (value: string) => value.split('');

const timeUnits = computed(() => [
	{ digits: splitDigits(getHours.value), label: 'Hours' },
	{ digits: splitDigits(getMinutes.value), label: 'Minutes' },
	{ digits: splitDigits(getSeconds.value), label: 'Seconds' },
]);
</script>

<style scoped>
.clock-card {
	display: flex;
	justify-content: center;
	align-items: center;
	background: transparent;
}

.digit-group {
	display: flex;
	align-items: center;
	justify-content: center;
	overflow: hidden;
}

.colon {
	font-size: 12vw;
	font-weight: bold;
	user-select: none;
}
</style>