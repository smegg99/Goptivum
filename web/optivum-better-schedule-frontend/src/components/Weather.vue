<template>
	<v-row class="d-flex justify-center align-center flex-nowrap" no-gutters>
		<v-card class="weather-card" flat>
			<v-row class="justify-center align-center weather-info">
				<v-col class="text-center" cols="6">
					<span class="location-info">Nowy Sącz</span>
					<div class="temp-info">
						<v-icon class="condition-icon">mdi-weather-lightning-rainy</v-icon>
						<span class="temperature">35°</span>
					</div>
				</v-col>
				<v-col class="text-right condition-col" cols="5">
					<div class="d-flex flex-column align-end">
						<span class="condition-text">Thundershower</span>
						<span class="condition-text">Lightly Polluted</span>
					</div>
				</v-col>
			</v-row>
			<v-divider></v-divider>
			<v-card-text class="forecast-section">
				<v-row justify="space-around" class="forecast mt-4">
					<v-col v-for="day in forecast" :key="day.name" class="text-center forecast-col">
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
.weather-card {
	width: 70%;
	padding: 1.5vw;
	background-color: transparent;
	user-select: none;
}

.weather-info {
	margin-bottom: 1vw;
}

.location-info {
	font-size: 3vw;
	font-weight: 600;
	white-space: nowrap;
	color: var(--v-primary-base);
	user-select: none;
}

.temp-info {
	display: flex;
	justify-content: center;
	align-items: center;
	margin-top: 0.5vw;
}

.condition-icon {
	font-size: 4vw;
	margin-right: 0.3vw;
	color: var(--v-warning-base);
	user-select: none;
}

.temperature {
	font-size: 3vw;
	font-weight: 700;
	color: var(--v-primary-darken1);
	user-select: none;
}

.condition-col {
	margin-top: 0.5vw;
}

.condition-text {
	font-size: 2vw;
	font-weight: 400;
	color: var(--v-secondary-lighten2);
	text-align: right;
	white-space: nowrap;
	user-select: none;
}

.forecast-section {
	padding: 1vw 0;
}

.forecast {
	display: flex;
	flex-wrap: nowrap;
}

.forecast-col {
	flex-grow: 1;
	margin: 0 0.5vw;
}

.forecast-day {
	font-size: 1.8vw;
	font-weight: 500;
	color: var(--v-secondary-lighten1);
	user-select: none;
}

.forecast-temp {
	font-size: 1.6vw;
	font-weight: 400;
	color: var(--v-secondary-darken1);
	user-select: none;
}

.forecast-icon {
	font-size: 2.5vw;
	color: var(--v-info-base);
	user-select: none;
}

.muted-text {
	color: var(--v-primary-lighten4);
}

@media (max-width: 800px) {
	.weather-card {
		width: 95%;
		padding: 1vw;
	}

	.location-info {
		font-size: 5vw;
	}

	.condition-icon {
		font-size: 6vw;
	}

	.temperature {
		font-size: 4vw;
	}

	.condition-text {
		font-size: 3vw;
	}

	.forecast-day,
	.forecast-temp {
		font-size: 4vw;
	}

	.forecast-icon {
		font-size: 6vw;
	}
}
</style>
