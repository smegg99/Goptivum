<template>
	<v-container>
		<v-table>
			<thead>
				<tr>
					<th>Przerwy</th>
					<th v-for="(day, index) in daysOfWeek" :key="index">{{ day }}</th>
				</tr>
			</thead>
			<tbody>
				<tr v-for="(timeRange, index) in uniqueTimeRanges" :key="index">
					<td>{{ formatTimeRange(timeRange) }}</td>
					<td v-for="dayIndex in 5" :key="dayIndex">
						<div v-if="filteredDuties(dayIndex - 1, timeRange).length > 0">
							<div v-for="duty in filteredDuties(dayIndex - 1, timeRange)" :key="duty.teacherFullName">
								{{ duty.teacherFullName }} - {{ duty.placeFullName }}
							</div>
						</div>
						<div v-else>—</div>
					</td>
				</tr>

			</tbody>
		</v-table>
	</v-container>
</template>

<script lang="ts" setup>
import { ref, computed, onMounted } from "vue";
import axios from "axios";

// API response data types
interface Timestamp {
	hour: number;
	minute: number;
}

interface TimeRange {
	start: Timestamp;
	end: Timestamp;
}

interface Duty {
	teacherFullName: string;
	placeFullName: string;
}

interface DutyGroup {
	duties: Duty[];
	timeRange: TimeRange;
}

interface TeachersOnDuty {
	dayOfWeek: string;
	dutyGroups: DutyGroup[];
}

interface ApiResponse {
	teachers_on_duty: TeachersOnDuty[];
}

// State
const dutiesData = ref<TeachersOnDuty[] | null>(null);

// Days of the week in Polish
const daysOfWeek = ["Poniedziałek", "Wtorek", "Środa", "Czwartek", "Piątek"];

// Fetch duties data
const fetchDuties = async () => {
	try {
		const response = await axios.get<ApiResponse>("/api/v1/teachers/duties"); // Adjust API endpoint
		if (response.data && Array.isArray(response.data.teachers_on_duty)) {
			dutiesData.value = response.data.teachers_on_duty;
		} else {
			console.error("Invalid API response format:", response.data);
			dutiesData.value = [];
		}
	} catch (error) {
		console.error("Failed to fetch duties:", error);
		dutiesData.value = [];
	}
};

// Get all unique time ranges
const uniqueTimeRanges = computed(() => {
	if (!dutiesData.value) return []; // Handle null or undefined state
	const timeSet = new Set<string>();
	dutiesData.value.forEach((day) => {
		day.dutyGroups.forEach((group) => {
			timeSet.add(JSON.stringify(group.timeRange));
		});
	});
	return Array.from(timeSet).map((range) => JSON.parse(range) as TimeRange);
});

// Filter duties for a specific day and time range
const filteredDuties = (dayIndex: number, timeRange: TimeRange) => {
	if (!dutiesData.value || !dutiesData.value[dayIndex]) return [];
	const dayDuties = dutiesData.value[dayIndex].dutyGroups || [];
	return dayDuties
		.filter(
			(group) =>
				group.timeRange.start.hour === timeRange.start.hour &&
				group.timeRange.start.minute === timeRange.start.minute &&
				group.timeRange.end.hour === timeRange.end.hour &&
				group.timeRange.end.minute === timeRange.end.minute
		)
		.flatMap((group) => group.duties);
};

// Format time range
const formatTimeRange = (range: TimeRange) => {
	const start = `${String(range.start.hour).padStart(2, "0")}:${String(
		range.start.minute
	).padStart(2, "0")}`;
	const end = `${String(range.end.hour).padStart(2, "0")}:${String(
		range.end.minute
	).padStart(2, "0")}`;
	return `${start} - ${end}`;
};

// Lifecycle hook
onMounted(fetchDuties);
</script>

<style scoped>
v-container {
	padding: 16px;
}

v-table {
	width: 100%;
	border-collapse: collapse;
}

thead {
	background-color: #f0f0f0;
}

td,
th {
	padding: 8px;
	text-align: left;
	border: 1px solid #ddd;
}
</style>
