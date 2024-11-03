<!-- Home.vue -->
<template>
	<!-- <div class="fill-height fill-width pa-0">
		<v-slide-y-reverse-transition appear>
			<div class="grid-container pa-0 fill-height">
				<v-row class="room-grid" align="center" justify="center">
					<ScheduleTable class="grid-item" :index="id" :type="type" />
				</v-row>
			</div>
		</v-slide-y-reverse-transition>
	</div> -->
	<ScheduleTable :type="type" />
</template>

<script setup lang="ts">
import ScheduleTable from '../ScheduleTable.vue';
const type = "room";
</script>

<!-- <style scoped>
.grid-container {
	display: grid;
	grid-template-rows: 1fr;
	align-items: center;
	justify-items: center;
	padding: 0;
	height: 100vh;
}

.room-grid {
	display: grid;
	grid-template-rows: auto;
	width: 100%;
	align-items: center;
	justify-items: center;
	gap: 0;
	padding: 0;
	margin: 0;
	height: 100%;
}

.grid-item {
	max-width: 100%;
	padding: 0;
}

@media (max-width: 1279px) {
	.grid-container {
		align-items: start;
		justify-items: stretch;
		height: auto;
		min-height: 100vh;
	}

	.division-grid {
		align-items: start;
		justify-items: start;
		grid-template-rows: auto;
		width: 100%;
		height: auto;
	}

	.grid-item {
		align-self: start;
		width: 100%;
		padding-top: 0;
	}
}
</style> -->