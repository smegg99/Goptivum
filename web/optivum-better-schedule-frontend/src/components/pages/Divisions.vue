<!-- Divisions.vue -->
<template>
	<v-container class="search-container" flui>
		<v-col class="divisions-grid" align="center" justify="center">
			<v-text-field v-model="search" class="search" label="Search Icons" placeholder="Type to search..."
				prepend-inner-icon="mdi-magnify" outlined></v-text-field>
		</v-col>
	</v-container>
	<v-slide-y-reverse-transition appear>
		<v-container class="grid-container" fluid>
			<v-row class="divisions-grid" align="center" justify="center">
				<v-col cols="12" class="d-flex justify-center grid-item">
					<v-row>
						<v-col v-for="(item, index) in filteredItems" :key="index" cols="4" sm="3" md="2" lg="1"
							class="d-flex align-center justify-center">
							<DivisionButton :text="item" />
						</v-col>
					</v-row>
				</v-col>
			</v-row>
		</v-container>
	</v-slide-y-reverse-transition>
</template>

<script setup>
import { ref, computed } from 'vue'
import DivisionButton from '../DivisionButton.vue';

// Reactive state
const search = ref('') // Holds the search input
const items = ref([
	'START', 'abacus', 'access-point', 'account', 'account-alert', 'account-box',
	'access-point-check', 'access-point-minus', 'access-point-network', 'access-point-off',
	'ab-testing', 'abacus', 'access-point', 'account', 'account-alert', 'account-box',
	'access-point-check', 'access-point-minus', 'access-point-network', 'access-point-off',
	'ab-testing', 'abacus', 'access-point', 'account', 'account-alert', 'account-box',
	'access-point-check', 'access-point-minus', 'access-point-network', 'access-point-off',
	'ab-testing', 'abacus', 'access-point', 'account', 'account-alert', 'account-box',
	'access-point-check', 'access-point-minus', 'access-point-network', 'access-point-off',
	'ab-testing', 'abacus', 'access-point', 'account', 'account-alert', 'account-box',
	'access-point-check', 'access-point-minus', 'access-point-network', 'access-point-off',
	'ab-testing', 'abacus', 'access-point', 'account', 'account-alert', 'account-box',
	'access-point-check', 'access-point-minus', 'access-point-network', 'access-point-off',
	'ab-testing', 'abacus', 'access-point', 'account', 'account-alert', 'account-box',
	'access-point-check', 'access-point-minus', 'access-point-network', 'access-point-off',
	'ab-testing', 'abacus', 'access-point', 'account', 'account-alert', 'account-box',
	'access-point-check', 'access-point-minus', 'access-point-network', 'access-point-off',
	'ab-testing', 'abacus', 'access-point', 'account', 'account-alert', 'account-box',
	'access-point-check', 'access-point-minus', 'access-point-network', 'END',
])

// Computed property for filtered items
const filteredItems = computed(() => {
	// If search is empty, show all items
	if (!search.value) return items.value
	// Filter items based on search input
	return items.value.filter(item => item.toLowerCase().includes(search.value.toLowerCase()))
})
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

.search-container {
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	overflow: hidden;
	position: static;
	padding: 0;
}

.divisions-grid {
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

.search {
	width: 100%;
	max-width: 500px;
	margin: 1rem;
}
</style>