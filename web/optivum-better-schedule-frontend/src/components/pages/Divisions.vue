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
				<v-col v-for="(designator, index) in filteredDivisions" :key="index" class="grid-item pa-0">
					<DivisionButton :text="designator.text" :index="index" :id="designator.id" />
				</v-col>
			</v-container>
		</v-container>
	</v-slide-y-reverse-transition>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue';
import axios from 'axios';
import { useI18n } from 'vue-i18n';

interface Division {
	text: string;
	id: number;
	full_name: string;
}

const { t } = useI18n();
const search = ref('');
const divisions = ref<Division[]>([]);
const loading = ref(false);
const error = ref<string | null>(null);

const fetchDivisions = async () => {
	loading.value = true;
	try {
		const response = await axios.get('/api/v1/divisions');
		const designators = response.data.designators;
		const fullNames = response.data.full_names;

		divisions.value = Object.keys(designators).map((key) => ({
			text: key,
			id: designators[key],
			full_name: Object.keys(fullNames).find((name) => fullNames[name] === designators[key]) || '',
		}));
	} catch (err) {
		console.error('Error fetching divisions:', err);
		error.value = 'Failed to fetch division data.';
	} finally {
		loading.value = false;
	}
};

onMounted(fetchDivisions);

const filteredDivisions = computed(() => {
	const searchValue = search.value.toLowerCase().replace(/\s+/g, '');
	return divisions.value.filter(
		(division) =>
			division.text.toLowerCase().includes(searchValue) ||
			division.full_name.toLowerCase().replace(/\s+/g, '').includes(searchValue)
	);
});
</script>

<style scoped lang="scss">
.main-container {
	display: flex;
	flex-direction: column;
	height: 100vh;
	background-color: rgb(var(--v-theme-background));
	overflow: visible;
}

:deep(.search .v-field--variant-solo) {
	box-shadow: none !important;
	border-color: transparent !important;
}

.search-container {
	width: 30%;
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
	width: auto;
	height: auto;
	overflow-y: auto;
	background-color: rgb(var(--v-theme-background));
	margin-bottom: 0;
}

.divisions-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(7rem, 1fr));
	column-gap: 2rem;
	row-gap: 2rem;
	justify-content: center;
	background-color: rgb(var(--v-theme-background));
	padding: 1.5rem;
	margin: 2rem 2rem 2rem;
	width: auto;
	height: auto;
}

.grid-item {
	width: 7rem;
	height: 7rem;
	display: flex;
	justify-content: center;
	align-items: center;
	aspect-ratio: 1 / 1;
	background-color: transparent;
}

@media (max-width: 1279px) {
	.search-container {
		width: calc(75% - 32px);
		max-width: 50vw;
		margin: 0px 0px 0 auto;
		height: 64px;
		padding: 0;
		justify-content: flex-end;
		position: fixed;
		top: 16px;
		right: 16px;
		z-index: 10;
		display: flex;
		align-items: center;
	}

	.scrollable-grid {
		margin-top: calc(64px + 16px);
	}

	.divisions-grid {
		grid-template-columns: repeat(auto-fill, minmax(7rem, 1rem));
		gap: 2rem;
		padding: 1rem;
		margin: 2rem 2rem 2rem;
	}
	
	.grid-item {
		width: 7rem;
		height: 7rem;
	}
}
</style>