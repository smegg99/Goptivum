<!-- pages/Teachers.vue -->
<template>
	<template v-if="!reducedAnimationsEnabled">
		<v-slide-y-transition appear>
			<v-card class="search-container pa-0" elevation="8" rounded="pill">
				<v-text-field v-model="search" class="search" :label="t('search.teacher')"
					prepend-inner-icon="mdi-magnify" variant="solo" rounded="pill" hide-details="auto"
					@input="debouncedSearch" />
			</v-card>
		</v-slide-y-transition>

		<v-slide-y-reverse-transition appear>
			<v-container class="scrollable-grid pa-0">
				<v-container :key="searchKey" class="teachers-grid pa-0">
					<v-col v-for="(teacher, index) in filteredTeachers" :key="teacher.id" class="grid-item pa-0"
						:class="{ 'animated-item': !reducedAnimationsEnabled }"
						:style="!reducedAnimationsEnabled ? delayStyle(index) : {}">
						<TeacherButton :text="teacher.full_name" :designator="teacher.designator" :index="index"
							:id="teacher.id" />
					</v-col>
				</v-container>

				<v-empty-state v-if="!loading && !error && filteredTeachers.length === 0"
					icon="mdi-magnify-remove-outline" class="no-teachers" :title="t('page.no_teachers')" />
				<v-empty-state v-if="error" icon="mdi-alert-circle" color="error" class="no-teachers"
					:title="t('page.teachers_error')" />
			</v-container>
		</v-slide-y-reverse-transition>
	</template>

	<template v-else>
		<v-card class="search-container pa-0" elevation="8" rounded="pill">
			<v-text-field v-model="search" class="search" :label="t('search.teacher')" prepend-inner-icon="mdi-magnify"
				variant="solo" rounded="pill" hide-details="auto" @input="debouncedSearch" />
		</v-card>

		<v-container class="scrollable-grid pa-0">
			<v-container :key="searchKey" class="teachers-grid pa-0">
				<v-col v-for="(teacher, index) in filteredTeachers" :key="teacher.id" class="grid-item pa-0">
					<TeacherButton :text="teacher.full_name" :designator="teacher.designator" :index="index"
						:id="teacher.id" />
				</v-col>
			</v-container>

			<v-empty-state v-if="!loading && !error && filteredTeachers.length === 0" icon="mdi-magnify-remove-outline"
				class="no-teachers" :title="t('page.no_teachers')" />
			<v-empty-state v-if="error" icon="mdi-alert-circle" color="error" class="no-teachers"
				:title="t('page.teachers_error')" />
		</v-container>
	</template>

	<div v-if="loading" class="loading">
		<v-progress-circular indeterminate></v-progress-circular>
	</div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue';
import axios from 'axios';
import { useI18n } from 'vue-i18n';
import { debounce } from 'lodash-es';
import TeacherButton from '@/components/TeacherButton.vue';
import { useMiscStore } from '@/stores/miscStore';

interface Teacher {
	designator: string;
	id: number;
	full_name: string;
}

interface TeacherResponse {
	data: TeacherResponseData;
}

interface TeacherResponseData {
	designators: { [key: string]: { values: number[] } };
	full_names: { [key: string]: { values: number[] } };
}

const { t } = useI18n();
const miscStore = useMiscStore();
const reducedAnimationsEnabled = computed(() => miscStore.reducedAnimationsEnabled);

const search = ref('');
const teachers = ref<Teacher[]>([]);
const loading = ref(false);
const error = ref<string | null>(null);

const fetchTeachers = async () => {
	loading.value = true;
	try {
		const response: TeacherResponse = await axios.get('/api/v1/teachers');
		const designators = response.data.designators;
		const fullNames = response.data.full_names;

		teachers.value = Object.entries(designators).map(([designator, { values }]) => {
			const id = values[0];
			const full_name = Object.keys(fullNames).find(name => fullNames[name].values.includes(id)) || '';

			return {
				designator,
				id,
				full_name,
			};
		});
	} catch (err) {
		console.error('Error fetching teachers:', err);
		error.value = 'Failed to fetch teacher data.';
	} finally {
		loading.value = false;
	}
};

const debouncedSearch = debounce(() => { }, 100);
const searchKey = computed(() => search.value);

const filteredTeachers = computed(() => {
	const searchValue = search.value.toLowerCase().trim();
	return teachers.value.filter(
		(teacher) =>
			teacher.designator.toLowerCase().includes(searchValue) ||
			teacher.full_name.toLowerCase().includes(searchValue)
	);
});

const delayStyle = (index: number) => ({
	animationDelay: `${index * 50}ms`,
});

onMounted(fetchTeachers);
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
	overflow: visible;
}

.teachers-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(12rem, 1fr));
	column-gap: clamp(0rem, 2vw, 1rem);
	row-gap: 2rem;
	justify-content: space-evenly;
	background-color: rgb(var(--v-theme-background));
	margin: 1rem 1rem 1rem;
	margin-top: 0;
	width: auto;
	height: auto;
	overflow: visible;
}

.grid-item {
	width: 12rem;
	height: 7rem;
	display: flex;
	align-items: center;
	justify-content: center;
	overflow: visible;
}

.animated-item {
	opacity: 0;
	transform: translateY(100%);
	animation: fadeInUp 0.5s forwards;
}

@keyframes fadeInUp {
	to {
		opacity: 1;
		transform: translateY(0);
	}
}

.animated-item.fade-leave-active {
	animation: fadeOutDown 0.5s forwards;
}

@keyframes fadeOutDown {
	from {
		opacity: 1;
		transform: translateY(0);
	}

	to {
		opacity: 0;
		transform: translateY(100%);
	}
}

.loading,
.error {
	display: flex;
	justify-content: center;
	align-items: center;
	padding: 1rem;
}

.no-teachers {
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	padding: 16px;
}

.no-teachers v-icon {
	margin-bottom: 8px;
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

	.teachers-grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(30%, 1fr));
		row-gap: 0rem;
		justify-content: center;
		padding: 1.5rem;
		margin: 0 1rem;
		width: auto;
		max-width: 100%;
	}

	.grid-item {
		width: 100%;
		aspect-ratio: 2 / 1;
		display: flex;
		align-items: center;
		justify-content: center;
		overflow: hidden;
		padding: 0.5rem;
		margin: 16px 0 16px 0;
		box-sizing: border-box;
	}
}

@media (max-width: 767px) {
	.teachers-grid {
		grid-template-columns: repeat(auto-fill, minmax(50%, 1fr));
	}
}
</style>
