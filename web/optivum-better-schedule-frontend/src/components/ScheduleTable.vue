<template>
	<v-slide-y-transition appear>
		<v-empty-state v-if="notFound" headline="404" :title="t('page.not_found')" />
	</v-slide-y-transition>

	<v-row class="division-grid" align="center" justify="center">
		<v-slide-y-transition appear>
			<div class="schedule-title-container pa-0">
				<span class="schedule-title">{{ title }}</span>
			</div>
		</v-slide-y-transition>
		<v-fade-transition appear>
			<div class="schedule-container grid-item pa-0">
				<!-- Mobile View -->
				<template v-if="isMobileView">
					<v-card variant="flat">
						<v-tabs v-model="activeTab" :style="{ backgroundColor: getRowColor(1) }" grow center-active>
							<v-tab v-for="(day, index) in availableDayNames" :key="index" class="schedule-day-tab">
								{{ day }}
							</v-tab>
						</v-tabs>
					</v-card>
					<v-tabs-items v-model="activeTab">
						<v-tab-item v-for="(day, dayIndex) in scheduleData?.schedule.schedule_days" :key="dayIndex">
							<v-table class="schedule-table" v-if="activeTab === dayIndex">
								<tbody>
									<tr v-for="(lessonGroup, groupIndex) in day.lesson_groups" :key="groupIndex"
										:style="{ backgroundColor: getRowColor(groupIndex) }">
										<td class="narrower-column"><span class="schedule-no">{{ groupIndex + 1
												}}.</span></td>
										<td class="narrow-column">
											<div class="stacked-time">
												<span class="schedule-time">{{
													formatTime(lessonGroup.lessons[0]?.time_range.start) }}</span>
												<span class="schedule-time">{{
													formatTime(lessonGroup.lessons[0]?.time_range.end) }}</span>
											</div>
										</td>

										<td class="schedule-table-data pa-0">
											<div v-for="lesson in lessonGroup.lessons" :key="lesson.full_name"
												class="stacked-lesson">
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
										</td>
									</tr>
								</tbody>
							</v-table>
						</v-tab-item>
					</v-tabs-items>
				</template>

				<!-- Desktop View -->
				<template v-else-if="scheduleData && !notFound">
					<v-table class="schedule-table">
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
								<td class="schedule-table-data"><span class="schedule-no">{{ rowIndex + 1 }}</span></td>
								<td class="schedule-table-data">
									<div class="stacked-time">
										<span class="schedule-time">{{ timeRange.split("-")[0] }}</span>
										<span class="schedule-time">{{ timeRange.split("-")[0] }}</span>
									</div>
								</td>
								<td v-for="(day, dayIndex) in scheduleData?.schedule.schedule_days" :key="dayIndex"
									class="schedule-table-data">
									<div
										v-if="day.lesson_groups.some((lg: LessonGroup) => formatTime(lg.lessons[0]?.time_range.start) + ' - ' + formatTime(lg.lessons[0]?.time_range.end) === timeRange)">
										<div v-for="lesson in day.lesson_groups.find((lg: LessonGroup) => formatTime(lg.lessons[0]?.time_range.start) + ' - ' + formatTime(lg.lessons[0]?.time_range.end) === timeRange)?.lessons"
											:key="lesson.full_name" class="stacked-lesson">
											<span class="schedule-lesson-name">{{ lesson.full_name }}</span>
											&nbsp;
											<router-link v-if="lesson.teacher_designator"
												:to="'/teacher/' + teacherIndexes[lesson.teacher_designator]"
												class="schedule-lesson-teacher">
												{{ lesson.teacher_designator }}
											</router-link>
											&nbsp;
											<router-link v-if="lesson.room_designator"
												:to="'/room/' + roomIndexes[lesson.room_designator]"
												class="schedule-lesson-room">
												{{ lesson.room_designator }}
											</router-link>
										</div>
									</div>
									<div v-else>&nbsp; <!-- Placeholder for empty cells --></div>
								</td>
							</tr>
						</tbody>
					</v-table>
				</template>
			</div>
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

const mobileViewBreakpoint = 895;

const scheduleData = ref<DivisionData | null>(null);
const title = computed(() => {
	const fullName = scheduleData.value?.full_name ?? '';
	const designator = scheduleData.value?.designator ? ` (${scheduleData.value.designator})` : '';
	return fullName + designator;
});
const teacherIndexes = ref<Record<string, number>>({});
const roomIndexes = ref<Record<string, number>>({});
const divisionIndexes = ref<Record<string, number>>({});
const notFound = ref(false);
const isMobileView = ref(window.innerWidth < mobileViewBreakpoint);
const activeTab = ref(0);

window.addEventListener("resize", () => {
	isMobileView.value = window.innerWidth < mobileViewBreakpoint;
});

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
.v-table {
	--v-table-header-height: 8px;
	--v-table-row-height: 4px;
}

.division-grid {
	flex-wrap: nowrap;
	display: grid;
	width: 100%;
	justify-items: center;
	align-items: center;
	gap: 0;
	padding: 0;
	margin: 0;
	margin-bottom: 4vh;
}

.grid-item {
	max-width: 100%;
	padding: 0;
}

.schedule-container {
	overflow-x: auto;
}

.schedule-table {
	width: 100%;
	table-layout: fixed;
}

.v-table td,
.v-table th {
	padding: 4px !important;
}

.schedule-table th,
.schedule-table td {
	text-align: left;
	overflow-wrap: break-word;
	word-break: break-word;
	font-size: 1vh;
	border: 1px solid rgba(255, 255, 255, 0.15);
	white-space: normal;
}

.schedule-title-container {
	width: 100%;
	height: auto;
	margin-top: 2vh;
	margin-bottom: 1vh;
	display: flex;
	justify-content: center;
	align-items: center;
	z-index: 10;
	position: sticky;
}

.schedule-title {
	font-size: 3vh;
	font-weight: 800;
	text-align: center;
	width: 100%;
	text-transform: uppercase;
	letter-spacing: 0.2em;
}

.schedule-no,
.schedule-time,
.schedule-head {
	font-size: 1.25vh;
	font-weight: 800;
	text-align: center;
	white-space: nowrap;
	display: flex;
	justify-content: center;
	align-items: center;
}

.schedule-time {
	font-weight: 600;
}

.schedule-lesson-name,
.schedule-lesson-teacher,
.schedule-lesson-room,
.schedule-lesson-division {
    font-size: 1.25vh;
    display: inline;
    white-space: nowrap;
}

.stacked-lesson {
	display: block;;
}

schedule-table td.schedule-table-data {
	max-width: 15vw;
	padding: 0px !important;
}

.division-grid {
	margin-top: calc(64px + 16px);
}

/* 
@media (max-width: 1279px) {
	.division-grid {
		margin-top: calc(64px + 16px);
	}
} */

@media (max-width: 895px) {
	.schedule-title-container {
        max-width: 90vw;
        margin: 0 auto;
        height: 64px;
        position: absolute;
        top: 16px;
        right: 16px;
    }

	.schedule-title {
		font-size: clamp(0.5rem, 4.5vw, 3.5rem);
		font-weight: 800;
		text-align: right;
		max-width: 100%;
		text-transform: uppercase;
		letter-spacing: 0.1em;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.schedule-container {
		width: 100vw;
		height: 100%;
		margin-top: calc(16px);
	}

	.schedule-table th.narrow-column,
	.schedule-table td.narrow-column {
		width: 15%;
		padding: 6px;
	}

	.schedule-table th.narrower-column,
	.schedule-table td.narrower-column {
		width: 5%;
		padding: 6px;
	}

	.stacked-time {
		display: flex;
		flex-direction: column;
		text-align: center;
	}

	.schedule-table-data {
		padding: 0px !important;
	}

	.schedule-table {
        font-size: 2vw;
    }

	.schedule-table th,
	.schedule-table td {
		font-size: 2vw;
		text-align: left;
		border: none;
	}

	.schedule-table-data {
		padding: 0px !important;
	}

	.schedule-no,
	.schedule-time,
	.schedule-head {
		padding-left: 0.4em;
		font-size: 1rem;
		font-weight: 600;
		text-align: left;
		white-space: nowrap;
		display: flex;
		justify-content: flex-start;
		align-items: center;
	}

	.schedule-no {
		width: 1em;
	}

	.schedule-lesson-teacher,
	.schedule-lesson-room,
	.schedule-lesson-division,
	.schedule-lesson-name {
		font-size: 1rem;
		font-weight: 400;
		white-space: nowrap;
		display: inline;
		text-align: left;
	}

	.schedule-day-tab {
		font-size: 1rem;
		font-weight: 800;
	}
}
</style>
