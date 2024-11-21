<!-- Home.vue -->
<template>
	<v-container class="grid-container pa-0 fill-height fill-width" fluid>
		<v-slide-x-reverse-transition appear>
			<div class="fabs-container">
				<v-btn icon="mdi-git" elevation="8" class="fab rounded-pill" color="secondary" @click="goToGitHub" />
			</div>
		</v-slide-x-reverse-transition>
		<v-slide-x-transition appear>
			<div class="fabs-container-bottom">
				<v-card elevation="8" class="fab-wide rounded-pill" color="primary">
					<span class="clients-count">{{ clientsCount }}</span>
				</v-card>
			</div>
		</v-slide-x-transition>
		<v-row class="home-grid">
			<v-col class="d-flex justify-center grid-item">
				<v-slide-y-transition appear>
					<Clock />
				</v-slide-y-transition>
			</v-col>
			<v-col class="d-flex justify-center grid-item">
				<v-slide-y-reverse-transition appear>
					<Weather />
				</v-slide-y-reverse-transition>
			</v-col>
		</v-row>
	</v-container>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue';

import Clock from '@/components/outputs/Clock.vue';
import Weather from '@/components/outputs/Weather.vue';

const goToGitHub = () => {
	window.open('https://github.com/smugg99/Goptivum');
};

let eventSource: EventSource | null = null;
const clientsCount = ref('');

const fetchClientsData = async () => {
	try {
		const response = await fetch('/api/v1/analytics/clients');
		const data = await response.json();
		clientsCount.value = data.message;
	} catch (error) {
		console.error('Failed to fetch clients data:', error);
	}
};

const setupSSE = () => {
	cleanupSSE();

	const endpoint = `/api/v1/events/clients`;
	eventSource = new EventSource(endpoint);

	eventSource.onmessage = (event) => {
		const index = parseInt(event.data, 10);
		fetchClientsData();
		console.log(`SSE message on ${endpoint}:`, index);
	};

	eventSource.onerror = (error) => {
		console.error(`SSE error on ${endpoint}:`, error);
		eventSource?.close();
	};
};

const cleanupSSE = () => {
	if (eventSource) {
		eventSource.close();
		eventSource = null;
	}
};

onMounted(() => {
	fetchClientsData();
	setupSSE();
});

onUnmounted(() => {
	cleanupSSE();
});
</script>

<style scoped>
.fabs-container-bottom,
.fabs-container {
	display: flex;
	flex-direction: row;
	gap: 16px;
	position: fixed;
	z-index: 100;
}

.fabs-container {
	top: 16px;
	right: 16px;
}

.fabs-container-bottom {
	bottom: 16px;
	right: 16px;
}

.fab {
	width: 56px;
	height: 56px;
	display: flex;
	align-items: center;
	justify-content: center;
}

.fab-wide {
	width: 56px;
	height: 28px;
	display: flex;
	align-items: center;
	justify-content: center;
}

.clients-count {
	font-size: 16px;
	font-weight: 900;
	user-select: none;
}

.grid-container {
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	overflow: hidden;
	padding: 0;
}

.home-grid {
	flex-wrap: nowrap;
	display: flex;
	flex-direction: column;
	width: 100%;
	justify-items: center;
	align-items: center;
	flex-grow: 0;
	gap: 5vh;
	padding: 0;
	margin: 0;
}

.grid-item {
	width: 90%;
	padding: 0;
}

.v-container {
	overflow-y: auto;
}

.v-container::-webkit-scrollbar {
	width: 0;
	height: 0;
}

.v-container {
	scrollbar-width: none;
}

@media (max-width: 1279px) {
	.fab {
		width: 64px;
		height: 64px;
	}

	.fab-wide {
		width: 64px;
		height: 32px;
	}

	.clients-count {
		font-size: 18px;
	}
}
</style>
