<template>
	<v-slide-y-transition appear mode="out-in">
		<v-empty-state v-if="notFound" headline="404" :title="t('page.not_found')" :key="transitionKey" />
	</v-slide-y-transition>

	<v-row class="division-grid" align="center" justify="center">
		<v-slide-x-reverse-transition appear mode="out-in">
			<div class="schedule-title-container pa-0" :key="transitionKey">
				<span class="schedule-title">{{ title }}</span>
			</div>
		</v-slide-x-reverse-transition>
		<v-fade-transition appear mode="out-in">
			<div class="schedule-container grid-item pa-0" :key="transitionKey">
				<!-- Mobile View -->
				<template v-if="isMobileView">
					<v-card variant="flat">
						<v-tabs v-model="activeTab" :style="{ backgroundColor: getRowColor(1) }" grow center-active>
							<v-tab v-for="(day, index) in availableDayNames" :key="index" class="schedule-day-tab">
								{{ day }}
							</v-tab>
						</v-tabs>
					</v-card>
					<v-window v-model="activeTab" touch :show-arrows="false">
						<v-window-item v-for="(day, dayIndex) in scheduleData?.schedule.schedule_days" :key="dayIndex">
							<v-table class="schedule-table" v-if="day.lesson_groups && day.lesson_groups.length > 0">
								<tbody>
									<tr v-for="(lessonGroup, groupIndex) in day.lesson_groups" :key="groupIndex" :class="[
										isCurrentLesson(lessonGroup, dayIndex)
											? currentLessonBackgroundClass
											: isNextLessonDuringBreak(day, groupIndex)
												? breakBackgroundClass
												: '',
									]" :style="!isCurrentLesson(lessonGroup, dayIndex) && !isNextLessonDuringBreak(day, groupIndex)
												? { backgroundColor: getRowColor(groupIndex) }
												: {}">
										<td class="narrower-column">
											<span class="schedule-no">{{ groupIndex + 1 }}.</span>
										</td>
										<td class="narrow-column">
											<div class="stacked-time">
												<span class="schedule-time">
													{{ formatTime(lessonGroup.lessons?.[0]?.time_range.start) }}
												</span>
												<span class="schedule-time">
													{{ formatTime(lessonGroup.lessons?.[0]?.time_range.end) }}
												</span>
											</div>
										</td>
										<td class="schedule-table-data pa-0">
											<div v-for="lesson in lessonGroup.lessons ?? []" :key="lesson.full_name"
												class="stacked-lesson">
												<span class="schedule-lesson-name">{{ lesson.full_name }}</span>
												<template v-if="showTeacherLink && lesson.teacher_designator">
													&nbsp;<router-link
														:to="'/teacher/' + teacherIndexes[lesson.teacher_designator]"
														class="schedule-lesson-teacher">
														{{ lesson.teacher_designator }}
													</router-link>&nbsp;
												</template>

												<template v-if="showDivisionLink && lesson.division_designator">
													&nbsp;<router-link
														:to="'/division/' + divisionIndexes[lesson.division_designator]"
														class="schedule-lesson-division">
														{{ lesson.division_designator }}
													</router-link>&nbsp;
												</template>

												<template v-if="showRoomLink && lesson.room_designator">
													&nbsp;<router-link
														:to="'/room/' + roomIndexes[lesson.room_designator]"
														class="schedule-lesson-room">
														{{ lesson.room_designator }}
													</router-link>&nbsp;
												</template>
											</div>
										</td>
									</tr>
								</tbody>
							</v-table>
							<v-empty-state v-else icon="mdi-calendar-remove" class="no-schedule"
								:title="t('page.no_schedule')" />
						</v-window-item>
					</v-window>
				</template>

				<!-- Desktop View -->
				<template v-else-if="scheduleData && !notFound">
					<v-slide-x-reverse-transition appear>
						<div class="fabs-container">
							<v-btn icon="mdi-qrcode" elevation="8" class="fab rounded-pill" color="primary"
								@click="generateQR" />
						</div>
					</v-slide-x-reverse-transition>
					<v-dialog v-model="qrDialog" max-width="400">
						<v-card rounded="xl">
							<v-card-text class="pa-0">
								<canvas ref="qrCodeContainer" style="display: block; margin: auto;"></canvas>
							</v-card-text>
							<v-card-actions>
								<v-btn color="primary" @click="qrDialog = false">
									{{ t('page.close') }}
								</v-btn>
							</v-card-actions>
						</v-card>
					</v-dialog>
					<v-table class="schedule-table">
						<thead>
							<tr>
								<th>
									<span class="schedule-head">{{ t('schedule.ordered_number') }}</span>
								</th>
								<th>
									<span class="schedule-head">{{ t('schedule.time_range') }}</span>
								</th>
								<th v-for="(dayName, index) in availableDayNames" :key="index">
									<span class="schedule-head">{{ dayName }}</span>
								</th>
							</tr>
						</thead>
						<tbody>
							<tr v-for="(timeRange, rowIndex) in uniqueTimeRanges" :key="rowIndex"
								:style="{ backgroundColor: getRowColor(rowIndex) }">
								<td class="schedule-table-data">
									<span class="schedule-no">{{ rowIndex + 1 }}</span>
								</td>
								<td class="schedule-table-data">
									<div class="stacked-time">
										<span class="schedule-time">{{ timeRange.split(' - ')[0] }}</span>
										<span class="schedule-time">{{ timeRange.split(' - ')[1] }}</span>
									</div>
								</td>
								<td v-for="(day, dayIndex) in scheduleData?.schedule.schedule_days" :key="dayIndex"
									class="schedule-table-data" :class="[
										{
											'current-lesson-cell': isCurrentLessonAtTime(day, timeRange, dayIndex),
											'lesson-after-break-cell': isNextLessonDuringBreakAtTime(
												day,
												timeRange,
												dayIndex
											),
										},
									]">
									<div v-if="
										day.lesson_groups &&
										day.lesson_groups.some(
											(lg: LessonGroup) =>
												lg.lessons &&
												formatTime(lg.lessons[0]?.time_range.start) +
												' - ' +
												formatTime(lg.lessons[0]?.time_range.end) ===
												timeRange
										)
									">
										<div v-for="lesson in (day.lesson_groups.find(
											(lg: LessonGroup) =>
												lg.lessons &&
												formatTime(lg.lessons[0]?.time_range.start) +
												' - ' +
												formatTime(lg.lessons[0]?.time_range.end) ===
												timeRange
										)?.lessons ?? [])" :key="lesson.full_name" class="stacked-lesson">
											<span class="schedule-lesson-name">{{ lesson.full_name }}</span>

											<template v-if="showTeacherLink && lesson.teacher_designator">
												&nbsp;<router-link
													:to="'/teacher/' + teacherIndexes[lesson.teacher_designator]"
													class="schedule-lesson-teacher">
													{{ lesson.teacher_designator }}
												</router-link>
											</template>

											<template v-if="showDivisionLink && lesson.division_designator">
												&nbsp;<router-link
													:to="'/division/' + divisionIndexes[lesson.division_designator]"
													class="schedule-lesson-division">
													{{ lesson.division_designator }}
												</router-link>
											</template>

											<template v-if="showRoomLink && lesson.room_designator">
												&nbsp;<router-link :to="'/room/' + roomIndexes[lesson.room_designator]"
													class="schedule-lesson-room">
													{{ lesson.room_designator }}
												</router-link>
											</template>
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
import { ref, onMounted, computed, watch, onUnmounted, nextTick } from 'vue';
import axios from 'axios';
import { useI18n } from 'vue-i18n';
import { useTheme } from 'vuetify';
import { useBackgroundGradientClass } from '@/composables/useThemeStyles';
import QRCode from 'qrcode';

const props = defineProps<{ id: string; type: 'teacher' | 'room' | 'division' }>();

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
	lessons?: Lesson[];
}

interface ScheduleDay {
	lesson_groups?: LessonGroup[];
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

interface IndexesResponse {
	designators: Record<string, { values: number[] }>;
	full_names: Record<string, { values: number[] }>;
}

const { t } = useI18n();
const theme = useTheme();
let eventSource: EventSource | null = null;

const mobileViewBreakpoint = 895;

const currentLessonBackgroundClass = useBackgroundGradientClass('schedule_current_lesson').value;
const breakBackgroundClass = useBackgroundGradientClass('schedule_break').value;

const qrDialog = ref(false);
const qrCodeContainer = ref<HTMLDivElement | null>(null);

const scheduleData = ref<DivisionData | null>(null);
const title = computed(() => {
	const fullName = scheduleData.value?.full_name ?? '';
	const designator = scheduleData.value?.designator ? ` (${scheduleData.value.designator})` : '';
	if (fullName === scheduleData.value?.designator) {
		if (props.type === 'room') {
			return t('schedule.room_title_prefix') + scheduleData.value.designator;
		}
		return fullName;
	}
	return fullName + designator;
});

// Used to force transition when the id changes
const transitionKey = computed(() => props.id);

const showTeacherLink = computed(() => props.type !== 'teacher');
const showDivisionLink = computed(() => props.type !== 'division');
const showRoomLink = computed(() => props.type !== 'room');

const teacherIndexes = ref<Record<string, number>>({});
const roomIndexes = ref<Record<string, number>>({});
const divisionIndexes = ref<Record<string, number>>({});

const notFound = ref(false);
const isMobileView = ref(window.innerWidth < mobileViewBreakpoint);
const activeTab = ref(0);

window.addEventListener('resize', () => {
	isMobileView.value = window.innerWidth < mobileViewBreakpoint;
	if (isMobileView.value) {
		qrCodeContainer.value = null;
		qrDialog.value = false;
	}
});

async function generateQR() {
	const currentUrl = window.location.href;
	try {
		qrDialog.value = true;
		await nextTick();

		if (!qrCodeContainer.value) {
			console.error('QR Code container (canvas) not found.');
			return;
		}

		const colors = theme.current.value.colors;

		await QRCode.toCanvas(qrCodeContainer.value, currentUrl, {
			errorCorrectionLevel: 'L',
			maskPattern: 0,
			color: {
				dark: colors.textPrimary,
				light: colors.surface,
			},
			width: 400,
			margin: 2,
		});
	} catch (error) {
		console.error('Failed to generate QR code:', error);
	}
}

const getRowColor = (rowIndex: number) => {
	const colors = theme.current.value.colors;
	return rowIndex % 2 === 0 ? colors.background : colors.surface;
};

function extractIndexes(data: Record<string, { values: number[] }>): Record<string, number> {
	const indexes: Record<string, number> = {};
	for (const key in data) {
		if (data[key].values && data[key].values.length > 0) {
			indexes[key] = data[key].values[0];
		}
	}
	return indexes;
}

const updateActiveTab = () => {
	if (scheduleData.value && scheduleData.value.schedule && scheduleData.value.schedule.schedule_days) {
		let dayIndex = currentDayIndex.value;
		if (dayIndex >= 5) {
			dayIndex = 0;
		}

		if (dayIndex >= scheduleData.value.schedule.schedule_days.length) {
			dayIndex = 0;
		}
		activeTab.value = dayIndex;
	}
};

const fetchData = async () => {
	try {
		const scheduleResponse = await axios.get(`/api/v1/${props.type}/${props.id}`);
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
			roomIndexes.value = extractIndexes((roomsResponse.data as IndexesResponse).designators);
			teacherIndexes.value = extractIndexes((teachersResponse.data as IndexesResponse).designators);
		} else if (props.type === 'teacher') {
			const [roomsResponse, divisionsResponse] = await Promise.all([
				axios.get(`/api/v1/rooms`),
				axios.get(`/api/v1/divisions`),
			]);
			roomIndexes.value = extractIndexes((roomsResponse.data as IndexesResponse).designators);
			divisionIndexes.value = extractIndexes((divisionsResponse.data as IndexesResponse).designators);
		} else if (props.type === 'room') {
			const [teachersResponse, divisionsResponse] = await Promise.all([
				axios.get(`/api/v1/teachers`),
				axios.get(`/api/v1/divisions`),
			]);
			teacherIndexes.value = extractIndexes((teachersResponse.data as IndexesResponse).designators);
			divisionIndexes.value = extractIndexes((divisionsResponse.data as IndexesResponse).designators);
		}

		updateActiveTab();
	} catch (err) {
		console.error('Error fetching data:', err);
		notFound.value = true;
	}
};

const setupSSE = () => {
	cleanupSSE();

	const endpoint = `/api/v1/events/${props.type}s`;
	eventSource = new EventSource(endpoint);

	eventSource.onmessage = (event) => {
		const index = parseInt(event.data, 10);

		if (index === parseInt(props.id, 10)) {
			console.log(`Received update for ${props.type} with index ${index}, refreshing data...`);
			fetchData();
		}
	};

	eventSource.onerror = (error) => {
		console.error(`SSE error on ${endpoint}:`, error);
		eventSource?.close();
	};
};

const cleanupSSE = () => {
	if (eventSource) {
		eventSource.close();
		eventSource = null;
	}
};

onMounted(() => {
	fetchData();
	setupSSE();
});

onUnmounted(() => {
	cleanupSSE();
});

watch(
	() => [props.id, props.type],
	() => {
		fetchData();
		setupSSE();
	}
);

const currentTime = ref(new Date());

// Method to set currentTime manually for testing
function setCurrentTime(year: number, month: number, day: number, hours: number, minutes: number) {
	currentTime.value = new Date(year, month - 1, day, hours, minutes);
}

// @ts-ignore
window.setCurrentTime = setCurrentTime;

const currentDayIndex = computed(() => {
	const day = currentTime.value.getDay(); // getDay(): 0 (Sunday) to 6 (Saturday)
	return day === 0 ? 6 : day - 1; // Adjust to 0 (Monday) to 6 (Sunday)
});

const isCurrentLesson = (lessonGroup: LessonGroup, dayIndex: number) => {
	if (dayIndex !== currentDayIndex.value) return false;

	if (!lessonGroup.lessons || lessonGroup.lessons.length === 0) return false;

	const now = currentTime.value;
	const currentTimeInMinutes = now.getHours() * 60 + now.getMinutes();

	const start = lessonGroup.lessons[0]?.time_range.start;
	const end = lessonGroup.lessons[0]?.time_range.end;

	if (!start || !end) return false;

	const lessonStartInMinutes = (start.hour || 0) * 60 + (start.minute || 0);
	const lessonEndInMinutes = (end.hour || 0) * 60 + (end.minute || 0);

	return currentTimeInMinutes >= lessonStartInMinutes && currentTimeInMinutes <= lessonEndInMinutes;
};

const isCurrentLessonAtTime = (day: ScheduleDay, timeRange: string, dayIndex: number) => {
	if (dayIndex !== currentDayIndex.value) return false;

	const now = currentTime.value;
	const currentTimeInMinutes = now.getHours() * 60 + now.getMinutes();

	const lessonGroup = day.lesson_groups?.find((lg) => {
		if (!lg.lessons || lg.lessons.length === 0) return false;
		const lessons = lg.lessons;
		const start = lessons[0]?.time_range.start;
		const end = lessons[0]?.time_range.end;
		if (!start || !end) return false;
		return `${formatTime(start)} - ${formatTime(end)}` === timeRange;
	});

	if (!lessonGroup || !lessonGroup.lessons || lessonGroup.lessons.length === 0) return false;

	const start = lessonGroup.lessons[0]?.time_range.start;
	const end = lessonGroup.lessons[0]?.time_range.end;

	if (!start || !end) return false;

	const lessonStartInMinutes = (start.hour || 0) * 60 + (start.minute || 0);
	const lessonEndInMinutes = (end.hour || 0) * 60 + (end.minute || 0);

	return currentTimeInMinutes >= lessonStartInMinutes && currentTimeInMinutes <= lessonEndInMinutes;
};

const getNextLessonIndexDuringBreak = (lessonGroups: LessonGroup[]) => {
	const now = currentTime.value;
	const currentTimeInMinutes = now.getHours() * 60 + now.getMinutes();

	for (let i = 0; i < lessonGroups.length; i++) {
		const lessonGroup = lessonGroups[i];
		if (!lessonGroup.lessons || lessonGroup.lessons.length === 0) continue;

		const lessons = lessonGroup.lessons;
		const start = lessons[0]?.time_range.start;
		const end = lessons[0]?.time_range.end;

		if (!start || !end) continue;

		const lessonStartInMinutes = (start.hour || 0) * 60 + (start.minute || 0);
		const lessonEndInMinutes = (end.hour || 0) * 60 + (end.minute || 0);

		if (currentTimeInMinutes < lessonStartInMinutes) {
			if (
				i === 0 ||
				currentTimeInMinutes >=
				((lessonGroups[i - 1].lessons?.[0]?.time_range.end.hour || 0) * 60 +
					(lessonGroups[i - 1].lessons?.[0]?.time_range.end.minute || 0))
			) {
				return i;
			}
		} else if (currentTimeInMinutes >= lessonStartInMinutes && currentTimeInMinutes <= lessonEndInMinutes) {
			return -1;
		}
	}
	return -1;
};

const isNextLessonDuringBreak = (day: ScheduleDay, index: number) => {
	if (currentDayIndex.value !== scheduleData.value?.schedule.schedule_days.indexOf(day)) return false;
	const nextLessonIndex = getNextLessonIndexDuringBreak(day.lesson_groups || []);
	return nextLessonIndex === index;
};

const getNextLessonDuringBreak = (day: ScheduleDay): LessonGroup | null => {
	const lessonGroups = day.lesson_groups;
	if (!lessonGroups) return null;

	const now = currentTime.value;
	const currentTimeInMinutes = now.getHours() * 60 + now.getMinutes();

	for (let i = 0; i < lessonGroups.length; i++) {
		const lessonGroup = lessonGroups[i];
		if (!lessonGroup.lessons || lessonGroup.lessons.length === 0) continue;

		const lessons = lessonGroup.lessons;
		const start = lessons[0]?.time_range.start;
		const end = lessons[0]?.time_range.end;

		if (!start || !end) continue;

		const lessonStartInMinutes = (start.hour || 0) * 60 + (start.minute || 0);
		const lessonEndInMinutes = (end.hour || 0) * 60 + (end.minute || 0);

		if (currentTimeInMinutes < lessonStartInMinutes) {
			if (
				i === 0 ||
				currentTimeInMinutes >=
				((lessonGroups[i - 1].lessons?.[0]?.time_range.end.hour || 0) * 60 +
					(lessonGroups[i - 1].lessons?.[0]?.time_range.end.minute || 0))
			) {
				return lessonGroup;
			}
		} else if (currentTimeInMinutes >= lessonStartInMinutes && currentTimeInMinutes <= lessonEndInMinutes) {
			return null;
		}
	}
	return null;
};

const isNextLessonDuringBreakAtTime = (day: ScheduleDay, timeRange: string, dayIndex: number) => {
	if (dayIndex !== currentDayIndex.value) return false;
	const nextLessonGroup = getNextLessonDuringBreak(day);
	if (!nextLessonGroup || !nextLessonGroup.lessons || nextLessonGroup.lessons.length === 0) return false;

	const start = nextLessonGroup.lessons[0]?.time_range.start;
	const end = nextLessonGroup.lessons[0]?.time_range.end;

	if (!start || !end) return false;

	const nextLessonTimeRange = `${formatTime(start)} - ${formatTime(end)}`;
	return nextLessonTimeRange === timeRange;
};

const uniqueTimeRanges = computed(() => {
	const timeSet = new Set<string>();
	scheduleData.value?.schedule.schedule_days.forEach((day) => {
		if (Array.isArray(day.lesson_groups)) {
			day.lesson_groups.forEach((lessonGroup) => {
				if (!lessonGroup.lessons || lessonGroup.lessons.length === 0) return;
				const lessons = lessonGroup.lessons;
				const timeRange = lessons[0]?.time_range;
				if (timeRange) {
					const formattedTimeRange = `${formatTime(timeRange.start)} - ${formatTime(timeRange.end)}`;
					timeSet.add(formattedTimeRange);
				}
			});
		}
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
		t('day.sunday'),
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

<style scoped lang="scss">
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
	// margin-bottom: 4vh;
}

.grid-item {
	max-width: 100%;
	padding: 0;
}

.fabs-container {
	display: flex;
	flex-direction: row;
	gap: 16px;
	position: fixed;
	top: 16px;
	right: 16px;
	z-index: 100;
}

.fab {
	width: 56px;
	height: 56px;
	display: flex;
	align-items: center;
	justify-content: center;
}

.menu-card {
	z-index: 999;
	width: 32px;
	aspect-ratio: 1 / 1;
	display: inline-flex;
	padding: 32px;
	align-items: center;
	justify-content: center;
	position: fixed;
	top: 16px;
	left: 16px;
}

.schedule-container {
	overflow-x: auto;
}

.schedule-table {
	width: 100%;
	table-layout: fixed;
	border: 2px solid rgb(var(--v-theme-scheduleBorder));
}

.v-table td,
.v-table th {
	padding: 8px !important;
	user-select: none;
}

.schedule-table th,
.schedule-table td {
	user-select: none;
	text-align: left;
	overflow-wrap: break-word;
	word-break: break-word;
	font-size: 1.5vh;
	border: 1px solid rgb(var(--v-theme-scheduleBorder));
	white-space: normal;
}

.schedule-title-container {
	width: 90%;
	height: auto;
	margin-top: 16px;
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
	user-select: none;
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
	margin: 0;
	padding: 0;
}

.schedule-lesson-name,
.schedule-lesson-teacher,
.schedule-lesson-room,
.schedule-lesson-division {
	font-size: 1.5vh;
	display: inline;
	text-decoration: none;
}

.schedule-lesson-teacher,
.schedule-lesson-room,
.schedule-lesson-division {
	color: rgb(var(--v-theme-scheduleLink));
	font-weight: 800;
	transition: color 0.3s ease;
}

.schedule-lesson-teacher:hover,
.schedule-lesson-room:hover,
.schedule-lesson-division:hover {
	color: rgb(var(--v-theme-scheduleLinkAlt));
}

.stacked-lesson {
	display: block;
}

.schedule-table td.schedule-table-data {
	max-width: 15vw;
	padding: 2px !important;
	padding-left: 6px !important;
	padding-right: 6px !important;
}

.division-grid {
	margin-top: calc(64px + 24px);
}

.current-lesson-cell {
	border: 2px solid rgb(var(--v-theme-scheduleCurrentLesson)) !important;
	box-shadow: 0 0 12px rgba(var(--v-theme-scheduleCurrentLesson), 0.5);
}

.lesson-after-break-cell {
	border: 2px solid rgb(var(--v-theme-scheduleCurrentBreak)) !important;
	box-shadow: 0 0 12px rgba(var(--v-theme-scheduleCurrentBreak), 0.5);
}

.stacked-time {
	display: flex;
	flex-direction: column;
	text-align: center;
	margin: 0;
	padding: 0;
	border: none;
	background: transparent;
}

@media (max-width: 1279px) {
	.fab {
		width: 64px;
		height: 64px;
	}
}

@media (max-width: 894px) {
	.current-lesson-row {
		background-color: rgb(var(--v-theme-scheduleCurrentLessonBackground));
	}

	.lesson-after-break-row {
		background-color: rgb(var(--v-theme-scheduleCurrentBreakBackground));
	}

	.current-lesson-row,
	.lesson-after-break-row {
		border: none !important;
	}

	.schedule-title-container {
		max-width: 60vw;
		margin: 0 auto;
		height: 64px;
		position: absolute;
		top: 16px;
		right: 16px;
	}

	.schedule-title {
		font-size: clamp(0.8rem, 3vw, 3.5rem);
		font-weight: 800;
		text-align: right;
		max-width: 100%;
		text-transform: uppercase;
		letter-spacing: clamp(0.1em, 0.15em, 0.2em);
		text-wrap: wrap;
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
		width: 5%;
		padding: 6px;
	}

	.schedule-table th.narrower-column,
	.schedule-table td.narrower-column {
		width: 5%;
		padding: 6px;
	}

	.schedule-table td.schedule-table-data {
		padding: 6px !important;
	}

	.schedule-table {
		font-size: 2vw;
		border: none !important;
	}

	.schedule-table th,
	.schedule-table td {
		font-size: 2vw;
		text-align: left;
		border: none !important;
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
		display: inline;
		text-align: left;
		white-space: nowrap;
	}

	.schedule-day-tab {
		font-size: 1rem;
		font-weight: 800;
	}
}

@media (max-width: 545px) {

	.schedule-table th.narrow-column,
	.schedule-table td.narrow-column {
		width: 10%;
		padding: 6px;
	}

	.schedule-table th.narrower-column,
	.schedule-table td.narrower-column {
		width: 8%;
		padding: 6px;
	}
}
</style>
