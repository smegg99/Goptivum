<template>
	<VContainer class="schedule-container">
		<VTable v-if="scheduleData && !error" class="schedule-table">
			<thead>
				<tr>
					<th>Nr</th>
					<th>Godz</th>
					<th v-for="(day, index) in ['Poniedziałek', 'Wtorek', 'Środa', 'Czwartek', 'Piątek']" :key="index">
						{{ day }}
					</th>
				</tr>
			</thead>
			<tbody>
				<tr v-for="(lessonGroup, rowIndex) in scheduleDays[0]?.lesson_groups" :key="rowIndex">
					<td>{{ rowIndex + 1 }}</td>
					<td>
						{{ lessonGroup.lessons[0]?.time_range.start.hour }}:{{
							lessonGroup.lessons[0]?.time_range.start.minute }} -
						{{ lessonGroup.lessons[0]?.time_range.end.hour }}:{{
							lessonGroup.lessons[0]?.time_range.end.minute }}
					</td>
					<td v-for="(day, dayIndex) in scheduleDays" :key="dayIndex">
						<div v-if="day.lesson_groups[rowIndex]?.lessons.length">
							<div v-for="(lesson, lessonIndex) in day.lesson_groups[rowIndex]?.lessons"
								:key="lessonIndex">
								{{ lesson.full_name }} <br />
								<small>{{ lesson.teacher_designator }} {{ lesson.room_designator }}</small>
							</div>
						</div>
					</td>
				</tr>
			</tbody>
		</VTable>

		<div v-else-if="error" class="error-message">{{ error }}</div>
		<VProgressCircular v-else indeterminate></VProgressCircular>
	</VContainer>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue';
import { defineProps } from 'vue';
import axios from 'axios';
import { VTable, VProgressCircular, VContainer } from 'vuetify/components';

interface Lesson {
	full_name: string;
	teacher_designator?: string;
	room_designator?: string;
	time_range: {
		start: {
			hour: number;
			minute: number;
		};
		end: {
			hour: number;
			minute: number;
		};
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

const props = defineProps<{ index: number }>();

const scheduleData = ref<DivisionData | null>(null);
const error = ref<string | null>(null);

const fetchSchedule = async () => {
	try {
		const response = await axios.get(`http://localhost:3001/api/v1/division/${props.index}`);
		scheduleData.value = response.data;
	} catch (err) {
		console.error('Error fetching schedule data:', err);
		error.value = 'Failed to fetch schedule data.';
	}
};

onMounted(fetchSchedule);

const scheduleDays = computed(() => {
	return scheduleData.value?.schedule?.schedule_days ?? [];
});
</script>

<style scoped>
.schedule-container {
	padding: 16px;
}

.schedule-table {
	width: 100%;
	border-collapse: collapse;
}

.schedule-table th,
.schedule-table td {
	padding: 8px;
	border: 1px solid #ddd;
	text-align: center;
}

.error-message {
	color: red;
	font-size: 16px;
	text-align: center;
}
</style>
