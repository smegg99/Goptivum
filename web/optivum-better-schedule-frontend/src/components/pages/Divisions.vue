<!-- Divisions.vue -->
<template>
	<v-container fluid class="fill-height fill-width pa-0">
		<v-slide-y-transition appear>
			<v-card class="search-container pa-0 elevation-4 rounded-b-lg">
				<v-text-field v-model="search" class="search" :label="t('search.division')"
					prepend-inner-icon="mdi-magnify" variant="outlined" />
			</v-card>
		</v-slide-y-transition>
		<v-slide-y-reverse-transition appear>
			<v-container class="scrollable-grid pa-0">
				<div class="divisions-grid">
					<v-col v-for="(item, index) in filteredItems" :key="index" class="grid-item pa-0" cols="auto">
						<DivisionButton :text="item" :index="index" />
					</v-col>
				</div>
			</v-container>
		</v-slide-y-reverse-transition>
	</v-container>
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
	overflow: hidden;
}

.search-container {
	width: 50%;
	margin: auto;
	display: flex;
	justify-content: center;
	align-items: center;
	z-index: 10;
	position: fixed;
	top: 0;
	left: 0;
	right: 0;
	padding: 15px 0;
}

.search {
	margin-left: 15px;
	margin-right: 15px;
	margin-top: 15px;
	margin-bottom: 0;
	user-select: none;
}

.scrollable-grid {
	height: calc(100vh - 70px - 24px);
	width: 100%;
	overflow: visible;
}

.divisions-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(8rem, 1fr));
	gap: 2rem;
	justify-content: center;
	padding: 2rem;
	margin-top: 3rem;
	overflow: visible;
}

.grid-item {
	width: 8rem;
	height: 8rem;
	display: flex;
	justify-content: center;
	align-items: center;
	aspect-ratio: 1 / 1;
}
</style>