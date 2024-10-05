<template>
	<v-container class="d-flex justify-center align-center">
		<v-row justify="center" align="center" class="bg-gray-900 rounded-lg">
			<v-col v-for="(unit, index) in timeUnits" :key="index" class="d-flex flex-column align-center">
				<div class="d-flex">
					<DigitalClockDigit v-for="(digit, digitIndex) in unit.digits" :key="digitIndex" :value="digit" />
				</div>
				<div class="text-gray-400 uppercase text-xs font-weight-bold">
					{{ unit.label }}
				</div>
			</v-col>
		</v-row>
	</v-container>
</template>

<script lang="ts" setup>
import { ref, computed } from 'vue';
import DigitalClockDigit from './Digit.vue';

const time = ref(new Date());

setInterval(() => {
	time.value = new Date();
}, 1000);

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
.bg-gray-900 {
	background-color: #1a202c;
}

.text-gray-400 {
	color: #cbd5e0;
}

.uppercase {
	text-transform: uppercase;
}

.font-weight-bold {
	font-weight: bold;
}
</style>