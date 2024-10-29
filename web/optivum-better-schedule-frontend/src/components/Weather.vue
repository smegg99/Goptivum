<!-- Weather.vue -->
<template>
  <v-row class="d-flex justify-center align-center flex-nowrap" no-gutters>
    <v-card class="weather-card" flat :class="{ loading: isLoading, transitioning: isTransitioning }">
      <v-row class="justify-center align-center weather-info">
        <v-col class="text-center" cols="6">
          <span class="location-info">{{ locationName }}</span>
          <div class="temp-info">
            <v-icon class="condition-icon">{{ conditionIcon }}</v-icon>
            <span class="temperature">{{ temperature }}</span>
          </div>
        </v-col>
        <v-col class="text-right condition-col" cols="6">
          <div class="d-flex flex-column align-end">
            <span class="condition-text">{{ conditionName }}</span>
            <span class="condition-text-lower">{{ conditionDescription }}</span>
          </div>
        </v-col>
      </v-row>
      <v-divider></v-divider>
      <v-card-text class="forecast-section">
        <v-row justify="space-around" class="forecast mt-4">
          <v-col v-for="(day, index) in forecastData" :key="index" class="text-center forecast-col">
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
import { ref, onMounted, onUnmounted, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import axios from 'axios';

const { t, locale } = useI18n();

const units = ref('metric');
const imperialLocales = ['en-US', 'en-LR', 'en-MM'];

const updateUnits = () => {
  units.value = imperialLocales.includes(locale.value) ? 'imperial' : 'metric';
};

updateUnits();

watch(
  () => locale.value,
  () => {
    updateUnits();
    fetchWeatherData();
  }
);

const isLoading = ref(true);
const isTransitioning = ref(false);

const placeholderCities = ['Atlantis', 'Gotham', 'Metropolis', 'El Dorado', 'Shangri-La'];
const randomCity = placeholderCities[Math.floor(Math.random() * placeholderCities.length)];
const locationName = ref(randomCity);

const temperature = ref(`${Math.floor(Math.random() * 30)}°`);
const conditionNames = ['Sunny', 'Cloudy', 'Rainy', 'Snowy'];
const randomCondition = conditionNames[Math.floor(Math.random() * conditionNames.length)];
const conditionName = ref(randomCondition);
const conditionDescription = ref('Fetching weather data');
const conditionIcon = ref(getConditionIcon(randomCondition));

interface ForecastDay {
  code: number;
  name: string;
  icon: string;
  temp: string;
}

const forecastData = ref<ForecastDay[]>(
  Array.from({ length: 3 }, (_, i) => {
    const dayNames = [
      t('day.monday'),
      t('day.tuesday'),
      t('day.wednesday'),
      t('day.thursday'),
      t('day.friday'),
      t('day.saturday'),
      t('day.sunday')
    ];
    const randomDay = dayNames[Math.floor(Math.random() * dayNames.length)];
    const randomTemp = `${Math.floor(Math.random() * 10)}°/${Math.floor(Math.random() * 30)}°`;
    const randomIcon = getConditionIcon(
      conditionNames[Math.floor(Math.random() * conditionNames.length)]
    );
    return {
      code: i,
      name: randomDay,
      icon: randomIcon,
      temp: randomTemp,
    };
  })
);

const fetchWeatherData = async () => {
  const startTime = performance.now();

  const cachedData = localStorage.getItem('weatherData');
  const cachedTime = localStorage.getItem('weatherDataFetchTime');
  const now = Date.now();
  const dataTTL = 30 * 60 * 1000; // 30 minutes

  let useCachedData = false;

  if (cachedData && cachedTime) {
    const cachedTimeNum = parseInt(cachedTime);
    if (now - cachedTimeNum < dataTTL) {
      useCachedData = true;
      const parsedData = JSON.parse(cachedData);
      processWeatherData(parsedData.currentData, parsedData.forecast);
      isLoading.value = false;
    }
  }

  if (!useCachedData) {
    isLoading.value = true;

    try {
      const currentResponse = await axios.get(
        `http://localhost:3001/api/v1/weather/current?units=${units.value}`
      );
      const forecastResponse = await axios.get(
        `http://localhost:3001/api/v1/weather/forecast?units=${units.value}`
      );

      const currentData = currentResponse.data;
      const forecast = forecastResponse.data.forecast;

      const dataToCache = { currentData, forecast };
      localStorage.setItem('weatherData', JSON.stringify(dataToCache));
      localStorage.setItem('weatherDataFetchTime', now.toString());

      const minLoadingTime = 1000 + Math.random() * 1000;
      const elapsedTime = performance.now() - startTime;
      const remainingTime = Math.max(minLoadingTime - elapsedTime, 0);

      setTimeout(() => {
        isTransitioning.value = true;
        processWeatherData(currentData, forecast);

        setTimeout(() => {
          isLoading.value = false;
          isTransitioning.value = false;
        }, 1000);
      }, remainingTime);
    } catch (error) {
      console.error('Error fetching weather data:', error);
      isLoading.value = false;
    }
  }
};

const processWeatherData = (currentData: any, forecast: any[]) => {
  locationName.value = currentData.name;

  conditionName.value = t(`weather.conditions.${currentData.condition.name.toLowerCase()}`);
  conditionDescription.value = t(
    `weather.conditions.${currentData.condition.description.toLowerCase()}`
  );

  const tempUnit = units.value === 'metric' ? '°C' : '°F';
  temperature.value = `${Math.round(currentData.temperature.current)}${tempUnit}`;
  conditionIcon.value = getConditionIcon(currentData.condition.name);

  forecastData.value = forecast.map((dayData: any) => {
    const dayName = t('day.' + getDayName(dayData.dayOfWeek));
    const temp = `${Math.round(dayData.temperature.min)}°/${Math.round(
      dayData.temperature.max
    )}°`;

    const icon = getConditionIcon(dayData.condition.name);

    return {
      code: dayData.dayOfWeek,
      name: dayName,
      icon: icon,
      temp: temp,
    };
  });
};

const getDayName = (dayOfWeek: number) => {
  const days = [
    'monday',
    'tuesday',
    'wednesday',
    'thursday',
    'friday',
    'saturday',
    'sunday',
  ];
  return days[dayOfWeek % 7];
};

function getConditionIcon(conditionName: string) {
  const conditionIcons: Record<string, string> = {
    Clear: 'mdi-weather-sunny',
    Clouds: 'mdi-weather-cloudy',
    Rain: 'mdi-weather-rainy',
    Snow: 'mdi-weather-snowy',
    Thunderstorm: 'mdi-weather-lightning',
    Drizzle: 'mdi-weather-hail',
    Mist: 'mdi-weather-fog',
    Fog: 'mdi-weather-fog',
    Haze: 'mdi-weather-hazy',
    Sunny: 'mdi-weather-sunny',
    Cloudy: 'mdi-weather-cloudy',
    Rainy: 'mdi-weather-rainy',
    Snowy: 'mdi-weather-snowy',
  };
  return conditionIcons[conditionName] || 'mdi-weather-sunny';
}

let intervalId: number | null = null;

onMounted(() => {
  fetchWeatherData();

  intervalId = setInterval(fetchWeatherData, 5 * 1000);
});

onUnmounted(() => {
  if (intervalId !== null) {
    clearInterval(intervalId);
  }
});
</script>

<style scoped>
.weather-card {
  width: 100%;
  padding: 1.5vw;
  background-color: transparent;
  user-select: none;
  transition: filter 1s ease, opacity 1s ease;
}

.loading {
  filter: blur(15px);
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
  font-weight: 400;
  color: var(--v-primary-darken1);
  user-select: none;
}

.condition-col {
  margin-top: 0.5vw;
}

.condition-text {
  font-size: 2vw;
  font-weight: 800;
  color: var(--v-secondary-lighten2);
  text-align: right;
  white-space: nowrap;
  user-select: none;
}

.condition-text-lower {
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
  font-size: 4vw;
  color: var(--v-info-base);
  user-select: none;
}

.muted-text {
  color: var(--v-primary-lighten4);
}

@media (max-width: 700px) {
  .weather-card {
    width: 100%;
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

  .condition-text-lower {
    font-size: 2.5vw;
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
