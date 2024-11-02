<!-- Home.vue -->
<template>
	<div class="fill-height fill-width pa-0">
		<v-slide-y-reverse-transition appear>
			<div class="grid-container pa-0" fluid>
				<v-row class="division-grid" align="center" justify="center">
					<ScheduleTable class="grid-item" :index="id" :type="type" />
				</v-row>
			</div>
		</v-slide-y-reverse-transition>
	</div>
</template>

<script setup lang="ts">
import ScheduleTable from '../ScheduleTable.vue';

import { useRoute } from 'vue-router';

const route = useRoute();
const id = Number(route.params.id);
const type = "division"
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

.division-grid {
	flex-wrap: nowrap;
	display: grid;
	width: 100%;
	justify-items: center;
	align-items: center;
	gap: 0;
	padding: 0;
	margin: 0;
}

.grid-item {
	max-width: 100%;
	padding: 0;
}
</style>
