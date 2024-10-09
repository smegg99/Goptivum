<!-- Settings.vue -->
<template>
	<v-slide-y-reverse-transition appear>
		<v-container class="grid-container" fluid>
			<v-row class="home-grid" align="center" justify="center">
				<v-col cols="12" class="d-flex justify-center grid-item">
					<ThemeSwitcher />
				</v-col>
			</v-row>
		</v-container>
	</v-slide-y-reverse-transition>
</template>

<script setup lang="ts">
import ThemeSwitcher from '../ThemeSwitcher.vue';
</script>
