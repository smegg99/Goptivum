<template>
	<div class="weather-terminal">
		<h2>Weather Report: {{ city }}</h2>
		<pre v-if="weatherData">{{ formattedWeather }}</pre>
		<p v-else>Loading weather data...</p>
		<p v-if="error" class="error">{{ error }}</p>
	</div>
</template>

<script lang="ts" setup>
import { ref, onMounted, computed } from 'vue';
import axios from 'axios';

// Define the structure of the weather data received from the API
interface WeatherResponse {
	current: string;
	forecast: ForecastResponse;
}

interface ForecastResponse {
	weather: WeatherDetails[];
}

interface WeatherDetails {
	hourly: HourlyForecast[];
}

interface HourlyForecast {
	time: string;
	weatherDesc: { value: string }[];
	temp_C: string;
	windSpeedKmph: string;
	precipMM: string;
}

// Props declaration
const props = defineProps<{
	city: string;
}>();

// Reactive state
const weatherData = ref<WeatherResponse | null>(null);
const error = ref<string | null>(null);

// Fetch detailed weather data from the API
const fetchWeather = async () => {
	try {
		// Fetch the current weather data
		const response = await axios.get(`https://wttr.in/${props.city}?format=%C+%t%n%w%u+%p+%P`);

		// Fetch the 3-day weather forecast data
		const forecastResponse = await axios.get(`https://wttr.in/${props.city}?format=j1`);

		weatherData.value = {
			current: response.data,
			forecast: forecastResponse.data,
		};
	} catch (err) {
		error.value = 'Error fetching weather data';
	}
};

// Function to format the weather data for terminal-like output
const formattedWeather = computed(() => {
	if (!weatherData.value) return '';

	const { current, forecast } = weatherData.value;
	const currentWeather = `Weather: ${current}`;

	// Format forecast data
	const forecastData = forecast.weather[0].hourly.map((hour: HourlyForecast) => {
		const time = new Date(hour.time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
		return `│ ${time} | ${hour.weatherDesc[0].value} ${hour.temp_C} °C | Wind: ${hour.windSpeedKmph} km/h | Rain: ${hour.precipMM} mm`;
	}).join('\n');

	return `
${currentWeather}
┌──────────────────────────────┐
│ Hourly Forecast              │
└──────────────────────────────┘
${forecastData}
Follow @igor_chubin wttr.in pyphoon wego
  `;
});

// Lifecycle hook to fetch weather data on mount
onMounted(() => {
	fetchWeather();
});
</script>

<style scoped>
.weather-terminal {
	background-color: black;
	color: white;
	padding: 1rem;
	border-radius: 5px;
	font-family: monospace;
	white-space: pre;
}

.error {
	color: red;
}
</style>
