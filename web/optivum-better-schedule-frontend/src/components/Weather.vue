<!-- Weather.vue -->
<template>
	<v-row class="d-flex justify-center align-center flex-nowrap" no-gutters>
		<v-card class="weather-card" flat>
			<v-card-title>
				<span class="location-info">Nowy Sącz</span>
			</v-card-title>
			<v-card-subtitle class="d-flex flex-column align-start">
				<div class="weather-info d-flex align-center mb-4">
					<v-icon class="condition-icon">mdi-weather-lightning-rainy</v-icon>
					<span class="temperature">35°</span>
				</div>
				<div class="d-flex flex-column">
					<span>Thundershower</span>
					<span class="text-muted">Lightly Polluted</span>
				</div>
			</v-card-subtitle>
			<v-divider></v-divider>
			<v-card-text>
				<v-row justify="space-around" class="forecast mt-4">
					<v-col v-for="day in forecast" :key="day.name" class="text-center muted-text">
						<div class="forecast-day">{{ day.name }}</div>
						<v-icon class="forecast-icon">{{ day.icon }}</v-icon>
						<div class="forecast-temp">{{ day.temp }}</div>
					</v-col>
				</v-row>
			</v-card-text>
		</v-card>
	</v-row>
</template>

<script setup lang="ts">
import { ref } from 'vue';

const forecast = ref([
	{ name: 'Tues', icon: 'mdi-weather-rainy', temp: '-2°/-10°' },
	{ name: 'Wed', icon: 'mdi-weather-cloudy', temp: '-2°/-10°' },
	{ name: 'Thur', icon: 'mdi-weather-cloudy', temp: '-2°/-10°' },
]);
</script>

<style scoped>
.location-info {
	font-size: 3vw;
}

.weather-card {
	width: 100%;
	height: 100%;
	padding: 2vw;
	background-color: transparent;
}

.weather-info {
	display: flex;
	justify-content: center;
	align-items: center;
}

.condition-icon {
	font-size: 2vw;
	margin-right: 0.5vw;
}

.temperature {
	font-size: 2vw;
	font-weight: bold;
}

.forecast-day,
.forecast-temp {
	font-size: 1.2vw;
	color: rgba(100, 100, 100, 0.8);
}

.forecast-icon {
	font-size: 2vw;
	color: rgba(100, 100, 100, 0.8);
}

.muted-text {
	color: rgba(100, 100, 100, 0.8);
}
</style>