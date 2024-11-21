<!-- pages/Settings.vue -->
<template>
	<v-container class="fill-height fill-width pa-0">
		<v-slide-y-reverse-transition appear>
			<v-container class="pa-0" fluid>
				<v-row class="home-grid" align="center" justify="center">
					<v-col class="d-flex justify-center grid-item">
						<ThemeSwitcher />
					</v-col>
					<v-col class="d-flex justify-center grid-item">
						<LanguageSwitcher />
					</v-col>
					<v-col class="d-flex justify-center grid-item">
						<AnimationsSwitcher />
					</v-col>
				</v-row>
			</v-container>
		</v-slide-y-reverse-transition>
	</v-container>
</template>

<script setup lang="ts">
import ThemeSwitcher from '@/components/inputs/ThemeSwitcher.vue';
import LanguageSwitcher from '@/components/inputs/LanguageSwitcher.vue';
import AnimationsSwitcher from '@/components/inputs/AnimationsSwitcher.vue';
</script>

<style scoped>
.home-grid {
	flex-wrap: nowrap;
	display: grid;
	width: 100%;
	justify-items: center;
	align-items: center;
	gap: 4vh;
	padding: 0;
	margin: 0;
}

.grid-item {
	max-width: 100%;
	padding: 0;
}
</style>
