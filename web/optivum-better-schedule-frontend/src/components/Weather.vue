<!-- Weather.vue -->
<template>
  <v-row class="d-flex justify-center align-center flex-nowrap" no-gutters>
    <v-card class="weather-card" flat>
      <v-row class="justify-center align-center weather-info">
        <v-col class="text-center" cols="4">
          <span class="location-info">{{ locationName }}</span>
          <div class="info">
            <v-icon class="condition-icon">{{ conditionIcon }}</v-icon>
            <span class="temperature">{{ temperature }}</span>
          </div>
        </v-col>

        <v-col class="text-right condition-col" cols="4">
          <div class="d-flex flex-column justify-center align-center">
            <span class="condition-text">{{ conditionName }}</span>
            <span class="condition-text-lower">{{ conditionDescription }}</span>
          </div>
        </v-col>

        <v-col class="text-center" cols="4">
          <div class="d-flex flex-column justify-center align-center">
            <span class="condition-text">{{ t('air_quality.quality') }}</span>
            <span class="condition-text-lower">{{ airQualityLabel }}</span>
          </div>
          <div class="info">
            <v-icon class="air-quality-icon">{{ airQualityIcon }}</v-icon>
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
import { ref, onMounted, onUnmounted } from 'vue';
import { useI18n } from 'vue-i18n';
import axios from 'axios';

const { t, locale } = useI18n();

const units = ref('metric');
const imperialLocales = ['en-US', 'en-LR', 'en-MM'];

const updateUnits = () => {
  units.value = imperialLocales.includes(locale.value) ? 'imperial' : 'metric';
};

updateUnits();

const placeholderCities = ['Atlantis', 'Memphis', 'Metropolis', 'El Dorado', 'Shangri-La'];
const randomCity = placeholderCities[Math.floor(Math.random() * placeholderCities.length)];

const conditionNames = ['thunderstorm', 'drizzle', 'rain', 'snow', 'clear'];
const conditionDescriptions = ['thunderstorm with light rain', 'heavy intensity drizzle', 'extreme rain', 'heavy snow', 'clear sky']
const randomConditionIndex = Math.floor(Math.random() * conditionNames.length);
const randomConditionName = conditionNames[randomConditionIndex];
const randomConditionDescription = conditionDescriptions[randomConditionIndex];

const locationName = ref(randomCity);
const isLoading = ref(true);

const temperature = ref(`${Math.floor(Math.random() * 30)}°`);

const conditionName = ref(t(`weather.conditions.${randomConditionName}`));
const conditionDescription = ref(t(`weather.conditions.${randomConditionDescription}`));
const conditionIcon = ref(getConditionIcon(randomConditionName));

const airQualityIndex = ref(0);
const airQualityLabel = ref('');
const airQualityIcon = ref('');


interface ForecastDay {
  code: number;
  name: string;
  icon: string;
  temp: string;
}

const dayNames = [
  t('day.monday'),
  t('day.tuesday'),
  t('day.wednesday'),
  t('day.thursday'),
  t('day.friday'),
  t('day.saturday'),
  t('day.sunday')
];

const forecastData = ref<ForecastDay[]>(
  Array.from({ length: 3 }, (_, i) => {
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

const CACHE_KEY = 'weatherData';
const CACHE_TIME_KEY = 'weatherDataFetchTime';
const CACHE_DURATION = 5 * 60 * 1000;

const fetchWeatherData = async (retryDelay = 1000) => {
  const now = Date.now();

  const cachedData = localStorage.getItem(CACHE_KEY);
  const cachedTime = localStorage.getItem(CACHE_TIME_KEY);

  if (cachedData && cachedTime && now - parseInt(cachedTime) < CACHE_DURATION) {
    const parsedData = JSON.parse(cachedData);
    processWeatherData(parsedData.currentData, parsedData.forecast);
    isLoading.value = false;
    return;
  }

  isLoading.value = true;

  try {
    const currentResponse = await axios.get(
      `/api/v1/weather/current?units=${units.value}`
    );
    const forecastResponse = await axios.get(
      `/api/v1/weather/forecast?units=${units.value}`
    );

    const currentData = currentResponse.data;
    const forecast = forecastResponse.data.forecast;

    localStorage.setItem(CACHE_KEY, JSON.stringify({ currentData, forecast }));
    localStorage.setItem(CACHE_TIME_KEY, now.toString());

    processWeatherData(currentData, forecast);
  } catch (error) {
    if (axios.isAxiosError(error) && error.response && error.response.status === 429) {
      console.warn('Too many requests. Retrying in', retryDelay / 1000, 'seconds.');
      setTimeout(() => fetchWeatherData(retryDelay * 1.5), retryDelay);
    } else {
      console.error('Error fetching weather data:', error);
      setTimeout(() => {
        isLoading.value = false;
      }, 1000);
    }
  } finally {
    if (isLoading.value) {
      setTimeout(() => {
        isLoading.value = false;
      }, 1000);
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
    const dayName = t('day.' + getDayName(dayData.dayOfWeek || 0));
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

function getAirQualityInfo(pm25: number, pm10: number) {
  const aqi = Math.max(pm25, pm10);

  if (aqi <= 12) {
    return { label: t('air_quality.conditions.very_good'), icon: 'mdi-emoticon-outline' };
  } else if (aqi <= 35.4) {
    return { label: t('air_quality.conditions.good'), icon: 'mdi-emoticon-neutral-outline' };
  } else if (aqi <= 55.4) {
    return { label: t('air_quality.conditions.moderate'), icon: 'mdi-emoticon-confused-outline' };
  } else if (aqi <= 150.4) {
    return { label: t('air_quality.conditions.unhealthy_for_sensitive_groups'), icon: 'mdi-emoticon-sad-outline' };
  } else if (aqi <= 250.4) {
    return { label: t('air_quality.conditions.very_unhealthy'), icon: 'mdi-emoticon-frown-outline' };
  } else {
    return { label: t('air_quality.conditions.hazardous'), icon: 'mdi-emoticon-dead-outline' };
  }
}

const fetchAirQualityData = async () => {
  try {
    const response = await axios.get('/api/v1/air/current');
    const components = response.data.components;
    const { label, icon } = getAirQualityInfo(components.pm2_5, components.pm10);

    airQualityIndex.value = Math.max(components.pm2_5, components.pm10);
    airQualityLabel.value = label;
    airQualityIcon.value = icon;
  } catch (error) {
    console.error('Error fetching air quality data:', error);
  }
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
  fetchAirQualityData();

  intervalId = setInterval(() => {
    fetchWeatherData();
    fetchAirQualityData();
  }, CACHE_DURATION);
});

onUnmounted(() => {
  if (intervalId !== null) {
    clearInterval(intervalId);
    intervalId = null;
  }
});
</script>

<style scoped>
.weather-card {
  width: 100%;
  background-color: transparent;
  user-select: none;
}

.weather-info {
  margin-bottom: 1vw;
}

.air-quality-info,
.location-info {
  font-size: 3vw;
  font-weight: 800;
  white-space: nowrap;
  user-select: none;
}

.info {
  display: flex;
  justify-content: center;
  align-items: center;
  margin-top: 0.5vw;
}

.air-quality-icon,
.condition-icon {
  font-size: 4vw;
  margin-right: 1vw;
  color: rgb(var(--v-theme-gradient2));
  user-select: none;
}

.temperature {
  font-size: 3vw;
  font-weight: 600;
  color: rgb(var(--v-theme-gradient2));
  user-select: none;
}

.condition-col {
  margin-top: 0.5vw;
}

.condition-text {
  font-size: 2vw;
  font-weight: 800;
  text-align: center;
  white-space: nowrap;
  user-select: none;
}

.condition-text-lower {
  font-size: 1.5vw;
  font-weight: 400;
  font-style: italic;
  color: rgb(var(--v-theme-gradient2));
  text-align: center;
  text-wrap-mode: auto;
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
  font-weight: 800;
  user-select: none;
}

.forecast-temp {
  font-size: 1.6vw;
  font-weight: 600;
  color: rgb(var(--v-theme-gradient1));
  user-select: none;
}

.forecast-icon {
  font-size: 4vw;
  color: rgb(var(--v-theme-gradient1));
  user-select: none;
}

@media (max-width: 1279px) {
  .weather-card {
    width: 100%;
  }

  .location-info {
    font-size: 4vw;
  }

  .air-quality-icon,
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
    font-size: 2vw;
  }

  .forecast-day,
  .forecast-temp {
    font-size: 2.5vw;
  }

  .forecast-icon {
    font-size: 6vw;
  }
}

@media (max-width: 600px) {
  .weather-card {
    width: 100%;
  }

  .location-info {
    font-size: 3vw;
  }

  .air-quality-icon,
  .condition-icon {
    font-size: 8vw;
  }

  .temperature {
    font-size: 4vw;
  }

  .condition-text {
    font-size: 3vw;
  }

  .condition-text-lower {
    font-size: 3vw;
  }

  .forecast-day,
  .forecast-temp {
    font-size: 4vw;
  }

  .forecast-icon {
    font-size: 8vw;
  }
}
</style>
