<!-- pages/ResourcePage.vue -->
<template>
	<div ref="rootElementRef" :key="route.path">
		<v-slide-y-transition appear>
			<v-card class="search-container pa-0" elevation="8" rounded="pill">
				<v-text-field v-model="search" class="search" :label="t(`search.${type}`)" clearable
					prepend-inner-icon="mdi-magnify" variant="solo" rounded="pill" hide-details="auto"
					@input="debouncedSearch" @click:clear="onClear" />
			</v-card>
		</v-slide-y-transition>

		<component :is="gridWrapper">
			<v-container class="scrollable-grid pa-0">
				<v-container :key="searchKey" class="resource-grid pa-0" :style="gridStyle">
					<v-col v-for="(item, index) in filteredItems" :key="item.id" class="grid-item pa-0"
						:class="{ 'animated-item': !reducedAnimationsEnabled }"
						:style="!reducedAnimationsEnabled ? delayStyle(index) : {}">
						<ResourceButton :text="item.full_name" :designator="item.designator" :index="index"
							:id="item.id" :type="type" />
					</v-col>
				</v-container>

				<v-empty-state v-if="!loading && !error && filteredItems.length === 0"
					:icon="`mdi-magnify-remove-outline`" class="no-resources" :title="t(`page.no_${type}s`)" />
				<v-empty-state v-if="error" icon="mdi-alert-circle" color="error" class="no-resources"
					:title="t(`page.${type}s_error`)" />
			</v-container>
		</component>

		<div v-if="loading" class="loading">
			<v-progress-circular indeterminate></v-progress-circular>
		</div>
	</div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed, ComputedRef, watch } from 'vue';
import { useRoute } from 'vue-router';
import axios from 'axios';
import { useI18n } from 'vue-i18n';
import { debounce } from 'lodash-es';
import { useMiscStore } from '@/stores/miscStore';

import ResourceButton from '@/components/inputs/ResourceButton.vue';

interface ResourceItem {
	designator: string;
	id: number;
	full_name: string;
}

interface ResourceResponseData {
	designators: Record<string, { values: number[] }>;
	full_names: Record<string, { values: number[] }>;
}

const props = defineProps<{
	type: 'division' | 'teacher' | 'room';
}>();

const { t } = useI18n();
const route = useRoute();
const miscStore = useMiscStore();

const search = ref<string>('');
const items = ref<ResourceItem[]>([]);
const loading = ref<boolean>(false);
const error = ref<string | null>(null);

const rootElementRef = ref<HTMLElement | null>(null);
const reducedAnimationsEnabled: ComputedRef<boolean> = computed(() => miscStore.reducedAnimationsEnabled);

const gridWrapper = computed(() => (reducedAnimationsEnabled.value ? 'div' : 'v-slide-y-reverse-transition'));

const mobileViewBreakpoint = 432;
const isMobileView = ref(window.innerWidth < mobileViewBreakpoint);

window.addEventListener('resize', () => {
	isMobileView.value = window.innerWidth < mobileViewBreakpoint;
});

const gridStyle = computed(() => {
	const minWidth = props.type === 'teacher' ? '10rem' : '6rem';
	return {
		display: 'grid',
		gridTemplateColumns: `repeat(auto-fill, minmax(${minWidth}, 1fr))`,
		gap: '16px',
		justifyItems: 'center',
	};
});

const fetchItems = async () => {
	loading.value = true;
	try {
		const response = await axios.get<ResourceResponseData>(`/api/v1/${props.type}s`);
		const { designators, full_names } = response.data;

		items.value = Object.entries(designators).map(([designator, { values }]) => {
			const id = values[0];
			const full_name = Object.keys(full_names).find(name => full_names[name].values.includes(id)) || '';
			return { designator, id, full_name };
		});
	} catch (err) {
		console.error(`Error fetching ${props.type}s:`, err);
		error.value = `Failed to fetch ${props.type} data.`;
	} finally {
		loading.value = false;
	}
};

const debouncedSearch = debounce(() => { }, 100);

const searchKey = computed(() => search.value);
const delayStyle = (index: number) => ({ animationDelay: `${index * 30}ms` });

const onClear = () => {
	search.value = '';
	items.value = [];
	fetchItems();
};

const filteredItems = computed(() => {
	const searchValue = search.value.toLowerCase().trim();
	return items.value.filter(
		(item) =>
			item.designator.toLowerCase().includes(searchValue) ||
			item.full_name.toLowerCase().includes(searchValue)
	);
});

watch(
	() => [props.type, route.path],
	() => {
		search.value = '';
		items.value = [];
		error.value = null;
		onClear();
		fetchItems();
	}
);

onMounted(fetchItems);
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
	position: static;
	top: 16px;
	padding: 0 1rem;
}

.search {
	width: 100%;
}

.scrollable-grid,
.resource-grid {
	width: auto;
	height: auto;
	overflow-y: auto;
	margin-bottom: 0;
	overflow: visible;
	justify-content: center;
	align-items: center;
}

.scrollable-grid {
	margin-bottom: 16px;
}

.resource-grid {
	width: calc(100% - 32px);
	margin: 0 auto;
	box-sizing: border-box;
	grid-template-columns: repeat(auto-fill, minmax(12rem, 1fr));
	gap: 16px;

	@media (max-width: 450px) {
		grid-template-columns: repeat(auto-fill, minmax(8rem, 1fr));
	}
}

.grid-item {
	display: flex;
	align-items: center;
	justify-content: center;
	box-sizing: border-box;
}

.animated-item {
	opacity: 0;
	transform: translateY(100%);
	animation: fadeInUp 0.3s forwards;
}

@keyframes fadeInUp {
	to {
		opacity: 1;
		transform: translateY(0);
	}
}

.loading,
.error,
.no-resources {
	display: flex;
	justify-content: center;
	align-items: center;
	padding: 1rem;
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
		margin-top: calc(64px + 32px);
	}
}
</style>