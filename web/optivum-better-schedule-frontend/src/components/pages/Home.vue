<!-- Home.vue -->
<template>
	<v-container class="grid-container pa-0" fluid>
		<v-row class="home-grid" align="center" justify="center">
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
</script>

<style scoped>
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
</style>
