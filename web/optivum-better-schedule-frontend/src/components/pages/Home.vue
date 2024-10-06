<!-- Home.vue -->
<template>
	<v-slide-y-reverse-transition appear>
		<v-container class="d-flex justify-center align-center" fluid>
			<Clock />
		</v-container>
	</v-slide-y-reverse-transition>
</template>

<script setup lang="ts">
import Clock from '../clock/Clock.vue';
</script>

<style scoped>
.v-container {
	padding: 2rem;
	max-width: 600px;
	height: 300px;
	/* Ensure there's enough space for centering */
	display: flex;
	justify-content: center;
	align-items: center;
}
</style>
