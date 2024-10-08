<!-- Home.vue -->
<template>
	<v-slide-y-reverse-transition appear>
		<v-container class="grid-container" fluid>
			<v-row class="home-grid" align="center" justify="center">
				<v-col cols="12" class="d-flex justify-center grid-item">
					<Clock />
				</v-col>
				<v-col cols="12" class="d-flex justify-center grid-item">
					<Weather />
				</v-col>
			</v-row>
		</v-container>
	</v-slide-y-reverse-transition>
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
	display: grid;
	width: 100%;
	justify-items: center;
	align-items: center;
	gap: 1rem;
	padding: 0;
}

.grid-item {
	max-width: 100%;
	padding: 0;
}
</style>
