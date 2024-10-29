<!-- DivisionsButton.vue -->
<template>
	<v-card class="card-box rounded-pill pa-8" elevation="8">
		<v-btn class="fill-height fill-width" :ripple="false" v-ripple>
			<span>{{ text }}</span>
		</v-btn>
	</v-card>
</template>

<script setup>
import { defineProps } from 'vue'

const props = defineProps({
	text: {
		type: String,
		required: true
	}
})
</script>

<style scoped>
.card-box {
	margin: 3vh;
	aspect-ratio: 1 / 1;
	display: inline-flex;
	align-items: center;
	justify-content: center;
}
</style>