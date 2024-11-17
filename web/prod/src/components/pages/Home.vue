<!-- Home.vue -->
<template>
	<v-container class="grid-container pa-0 fill-height fill-width" fluid>
		<v-slide-x-reverse-transition appear>
			<div class="fabs-container">
				<v-btn icon="mdi-git" elevation="8" class="fab rounded-pill" color="secondary" @click="goToGitHub" />
			</div>
		</v-slide-x-reverse-transition>
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
import Clock from '../clock/Clock.vue';
import Weather from '../Weather.vue';

const goToGitHub = () => {
	window.open('https://github.com/smugg99/Goptivum');
};
</script>

<style scoped>
.fabs-container {
	display: flex;
	flex-direction: row;
	gap: 16px;
	position: fixed;
	top: 16px;
	right: 16px;
	z-index: 100;
}

.fab {
	width: 56px;
	height: 56px;
	display: flex;
	align-items: center;
	justify-content: center;
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
}
</style>
