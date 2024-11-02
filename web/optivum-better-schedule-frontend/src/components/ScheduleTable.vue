<template>
	<v-slide-y-transition appear>
		<v-empty-state v-if="notFound" headline="404" :title="t('page.not_found')" />
	</v-slide-y-transition>

	<v-row class="division-grid" align="center" justify="center">
		<v-slide-y-transition appear>
			<div class="schedule-title-container grid-item pa-0">
				<span class="schedule-title">{{ title }}</span>
			</div>
		</v-slide-y-transition>
		<v-fade-transition appear>
			<v-card class="schedule-container grid-item pa-0" variant="outlined" elevation="8">
				<v-table v-if="scheduleData && !notFound" class="schedule-table">
					<thead>
						<tr>
							<th><span class="schedule-head">{{ t('schedule.ordered_number') }}</span></th>
							<th><span class="schedule-head">{{ t('schedule.time_range') }}</span></th>
							<th v-for="(day, index) in availableDayNames" :key="index">
								<span class="schedule-head">{{ day }}</span>
							</th>
						</tr>
					</thead>
					<tbody>
						<tr v-for="(timeRange, rowIndex) in uniqueTimeRanges" :key="rowIndex"
							:style="{ backgroundColor: getRowColor(rowIndex) }">
							<td :style="{ backgroundColor: getRowColor(1) }"><span class="schedule-no">{{
									rowIndex + 1 }}</span></td>
							<td><span class="schedule-time">{{ timeRange }}</span></td>
							<td v-for="(day, dayIndex) in scheduleData?.schedule.schedule_days" :key="dayIndex">
								<div
									v-if="day.lesson_groups.some((lg: LessonGroup) => formatTime(lg.lessons[0]?.time_range.start) + ' - ' + formatTime(lg.lessons[0]?.time_range.end) === timeRange)">
									<div v-for="lesson in day.lesson_groups.find((lg: LessonGroup) => formatTime(lg.lessons[0]?.time_range.start) + ' - ' + formatTime(lg.lessons[0]?.time_range.end) === timeRange)?.lessons"
										:key="lesson.full_name">
										<span class="schedule-lesson-name">{{ lesson.full_name }}</span>&nbsp;

										<router-link
											v-if="lesson.teacher_designator && teacherIndexes[lesson.teacher_designator] !== undefined"
											:to="'/teacher/' + teacherIndexes[lesson.teacher_designator]"
											class="schedule-lesson-teacher">
											&nbsp;{{ lesson.teacher_designator }}
										</router-link>

										<router-link
											v-if="lesson.room_designator && roomIndexes[lesson.room_designator] !== undefined"
											:to="'/room/' + roomIndexes[lesson.room_designator]"
											class="schedule-lesson-room">
											&nbsp;{{ lesson.room_designator }}
										</router-link>

										<router-link
											v-if="lesson.division_designator && divisionIndexes[lesson.division_designator] !== undefined"
											:to="'/division/' + divisionIndexes[lesson.division_designator]"
											class="schedule-lesson-division">
											&nbsp;{{ lesson.division_designator }}
										</router-link>
									</div>
								</div>
								<div v-else>&nbsp; <!-- Placeholder for empty cells --></div>
							</td>
						</tr>
					</tbody>
				</v-table>
			</v-card>
		</v-fade-transition>
	</v-row>
</template>

<script setup lang="ts">
import { ref, onMounted, computed, defineProps } from 'vue';
import axios from 'axios';
import { useI18n } from 'vue-i18n';
import { useTheme } from 'vuetify';

const props = defineProps<{ index: number; type: 'teacher' | 'room' | 'division' }>();

interface TimeRange {
	hour?: number;
	minute?: number;
}

interface Lesson {
	full_name: string;
	teacher_designator?: string;
	room_designator?: string;
	division_designator?: string;
	time_range: {
		start: TimeRange;
		end: TimeRange;
	};
}

interface LessonGroup {
	lessons: Lesson[];
}

interface ScheduleDay {
	lesson_groups: LessonGroup[];
}

interface Schedule {
	schedule_days: ScheduleDay[];
}

interface DivisionData {
	index: number;
	designator: string;
	full_name: string;
	schedule: Schedule;
}

const { t } = useI18n();
const theme = useTheme();

const scheduleData = ref<DivisionData | null>(null);
const title = computed(() => scheduleData.value?.full_name ?? '');
const teacherIndexes = ref<Record<string, number>>({});
const roomIndexes = ref<Record<string, number>>({});
const divisionIndexes = ref<Record<string, number>>({});
const notFound = ref(false);

const getRowColor = (rowIndex: number) => {
	const colors = theme.current.value.colors;
	return rowIndex % 2 === 0 ? colors.background : colors.surface;
};

const fetchData = async () => {
	try {
		const scheduleResponse = await axios.get(`/api/v1/${props.type}/${props.index}`);
		if (scheduleResponse.status === 404) {
			notFound.value = true;
			return;
		}

		scheduleData.value = scheduleResponse.data;

		if (props.type === 'division') {
			const [roomsResponse, teachersResponse] = await Promise.all([
				axios.get(`/api/v1/rooms`),
				axios.get(`/api/v1/teachers`),
			]);
			roomIndexes.value = roomsResponse.data.designators;
			teacherIndexes.value = teachersResponse.data.designators;
		} else if (props.type === 'teacher') {
			const [roomsResponse, divisionsResponse] = await Promise.all([
				axios.get(`/api/v1/rooms`),
				axios.get(`/api/v1/divisions`),
			]);
			roomIndexes.value = roomsResponse.data.designators;
			divisionIndexes.value = divisionsResponse.data.designators;
		} else if (props.type === 'room') {
			const [teachersResponse, divisionsResponse] = await Promise.all([
				axios.get(`/api/v1/teachers`),
				axios.get(`/api/v1/divisions`),
			]);
			teacherIndexes.value = teachersResponse.data.designators;
			divisionIndexes.value = divisionsResponse.data.designators;
		}
	} catch (err) {
		console.error('Error fetching data:', err);
		notFound.value = true;
	}
};

onMounted(fetchData);

const uniqueTimeRanges = computed(() => {
	const timeSet = new Set<string>();
	scheduleData.value?.schedule.schedule_days.forEach((day) => {
		day.lesson_groups.forEach((lessonGroup) => {
			const timeRange = lessonGroup.lessons[0]?.time_range;
			if (timeRange) {
				const formattedTimeRange = `${formatTime(timeRange.start)} - ${formatTime(timeRange.end)}`;
				timeSet.add(formattedTimeRange);
			}
		});
	});
	return Array.from(timeSet).sort();
});

const availableDayNames = computed(() => {
	const dayNames = [
		t('day.monday'),
		t('day.tuesday'),
		t('day.wednesday'),
		t('day.thursday'),
		t('day.friday'),
		t('day.saturday'),
		t('day.sunday')
	];
	return dayNames.slice(0, scheduleData.value?.schedule.schedule_days.length ?? 0);
});

function formatTime(time: TimeRange | undefined): string {
	if (!time) return '00:00';
	const hours = time.hour !== undefined ? String(time.hour).padStart(2, '0') : '00';
	const minutes = time.minute !== undefined ? String(time.minute).padStart(2, '0') : '00';
	return `${hours}:${minutes}`;
}

</script>

<style scoped>
.division-grid {
	flex-wrap: nowrap;
	display: grid;
	width: 100%;
	justify-items: center;
	align-items: center;
	gap: 0;
	padding: 0;
	margin: 0;
}

.grid-item {
	max-width: 100%;
	padding: 0;
}

.schedule-container {
	width: 100%;
	overflow-x: auto;
}

.schedule-table {
	width: 100%;
	height: auto;
	border-collapse: collapse;
	table-layout: fixed;
	font-size: 1vw;
}

.schedule-table th,
.schedule-table td {
	padding: 0.5vw;
	text-align: center;
	overflow-wrap: break-word;
	font-size: 1vw;
	border: 1px solid rgba(255, 255, 255, 0.15);
}

.schedule-title-container {
	width: 100%;
	height: auto;
	margin: 1vw auto;
	display: flex;
	justify-content: center;
	align-items: center;
	z-index: 10;
	position: sticky;
	top: 1vw;
	padding: 0 1vw;
}

.schedule-title {
	font-size: 2rem;
	font-weight: 800;
	text-align: center;
	display: flex;
	justify-content: center;
	align-items: center;
	width: 100%;
	text-transform: uppercase;
	letter-spacing: 0.1em;
}

.schedule-no,
.schedule-time,
.schedule-head {
	font-size: 1.2rem;
	font-weight: 700;
	text-align: center;
	white-space: nowrap;
	display: flex;
	justify-content: center;
	align-items: center;
}

.schedule-time {
	font-weight: 600;
}

.lesson-info {
	display: inline-flex;
	flex-wrap: nowrap;
	align-items: center;
}

.schedule-lesson-teacher,
.schedule-lesson-room,
.schedule-lesson-division,
.schedule-lesson-name {
	font-size: 1rem;
	white-space: nowrap;
	display: inline;
}

@media (max-width: 1279px) {
	.division-grid {
		margin-top: calc(64px + 16px);;
	}

	.schedule-title {
		font-size: 3rem;
	}

	.schedule-table {
		font-size: 2vw;
	}

	.schedule-table th,
	.schedule-table td {
		padding: 0.8vw;
		font-size: 2vw;
	}

	.schedule-title {
		font-size: 4vw;
	}
}
</style>
