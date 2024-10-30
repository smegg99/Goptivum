<!-- Divisions.vue -->
<template>
	<v-slide-y-transition appear>
		<v-card class="search-container pa-0" elevation="8" rounded="pill">
			<v-text-field v-model="search" class="search" :label="t('search.division')" prepend-inner-icon="mdi-magnify"
				variant="solo" rounded="pill" hide-details="auto" />
		</v-card>
	</v-slide-y-transition>
	<v-slide-y-reverse-transition appear>
		<v-container class="scrollable-grid pa-0">
			<v-container class="divisions-grid pa-0">
				<v-col v-for="(item, index) in filteredItems" :key="index" class="grid-item pa-0" cols="auto">
					<DivisionButton :text="item" :index="index" />
				</v-col>
			</v-container>
		</v-container>
	</v-slide-y-reverse-transition>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n';
import DivisionButton from '../DivisionButton.vue';

const { t, locale } = useI18n();

const search = ref('')
const items = ref([
	'1M', '2M', '3M', '4M', '5M',
	'1F', '2F', '3F', '4F', '5F',
	'1A', '2A', '3A', '4A', '5A',
	'1B', '2B', '3B', '4B', '5B',
	'1C', '2C', '3C', '4C', '5C',
	'1D', '2D', '3D', '4D', '5D',
	'1E', '2E', '3E', '4E', '5E',
	'1G', '2G', '3G', '4G', '5G',
	'1H', '2H', '3H', '4H', '5H',
	'1I', '2I', '3I', '4I', '5I',
	'1J', '2J', '3J', '4J', '5J',
	'1K', '2K', '3K', '4K', '5K',
	'1L', '2L', '3L', '4L', '5L',
	'1N', '2N', '3N', '4N', '5N',
	'1O', '2O', '3O', '4O', '5O',
	'1P', '2P', '3P', '4P', '5P',
	'1R', '2R', '3R', '4R', '5R',
	'1S', '2S', '3S', '4S', '5S',
	'1T', '2T', '3T', '4T', '5T',
	'1U', '2U', '3U', '4U', '5U',
	'1W', '2W', '3W', '4W', '5W',
	'1X', '2X', '3X', '4X', '5X',
	'1Y', '2Y', '3Y', '4Y', '5Y',
	'1Z', '2Z', '3Z', '4Z', '5Z',
])

const filteredItems = computed(() => {
	if (!search.value) return items.value
	return items.value.filter(item => item.toLowerCase().includes(search.value.toLowerCase()))
})
</script>

<style scoped>
.main-container {
	display: flex;
	flex-direction: column;
	height: 100vh;
	overflow: visible;
}

:deep(.search .v-field--variant-solo) {
	box-shadow: none !important;
	border-color: transparent !important;
}

.search-container {
	width: 50%;
	height: 64px;
	margin: 16px auto;
	display: flex;
	justify-content: center;
	align-items: center;
	z-index: 10;
	position: sticky;
	top: 16px;
	padding: 0 1rem;
}

.search {
	width: 100%;
}

.v-card {
	overflow: visible;
}

.scrollable-grid {
	overflow-y: auto;
	width: auto;
	background-color: var(--v-background-base);
	padding: 1rem;
}

.divisions-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(7rem, 1fr));
	gap: 2rem;
	justify-content: center;
	background-color: var(--v-background-base);
	padding: 1.5rem;
	margin: 2rem 2rem 2rem;
	width: auto;
}

.grid-item {
	width: 7rem;
	height: 7rem;
	display: flex;
	justify-content: center;
	align-items: center;
	aspect-ratio: 1 / 1;
}

@media (max-width: 1280px) {
	.search-container {
		padding: 0.75rem;
	}

	.divisions-grid {
		grid-template-columns: repeat(auto-fill, minmax(5rem, 1fr));
		gap: 2rem;
		padding: 1rem;
		margin: 2rem 2rem 2rem;
	}

	.grid-item {
		width: 5rem;
		height: 5rem;
	}
}
</style>