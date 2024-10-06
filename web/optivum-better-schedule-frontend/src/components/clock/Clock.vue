<!-- Clock.vue -->
<template>
	<v-row class="d-flex justify-center align-center flex-nowrap" no-gutters>
		<v-card class="clock-card d-flex flex-nowrap elevation-8 rounded-pill">
			<v-col v-for="(unit, index) in timeUnits" :key="index" class="d-flex align-center" cols="auto">
				<div class="digit-group">
					<DigitalClockDigit v-for="(digit, digitIndex) in unit.digits" :key="digitIndex" :value="digit" />
					<span v-if="index < timeUnits.length - 1" class="colon">:</span>
				</div>
			</v-col>
		</v-card>
	</v-row>
</template>

<script lang="ts" setup>
import { ref, computed, onMounted, onUnmounted } from 'vue';
import DigitalClockDigit from './Digit.vue';

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
	padding: 2rem;
}

.digit-group {
	display: flex;
	align-items: center;
	justify-content: center;
	/* Center the digits */
	overflow: visible;
}

.v-row {
	flex-grow: 1;
	/* Allow row to expand */
}

.colon {
	font-size: 12vw;
	font-weight: bold;
	user-select: none;
	line-height: 1;
}
</style>