<!-- app/pages/data.vue -->

<script setup lang="ts">
const ws = useWorkspace()

onMounted(() => ws.reload())

const tab = ref('subjects')
const importError = ref('')

const tabs = [
  { value: 'calendar', title: 'Calendar' },
  { value: 'years', title: 'Years' },
  { value: 'divisions', title: 'Divisions' },
  { value: 'groups', title: 'Groups' },
  { value: 'teachers', title: 'Teachers' },
  { value: 'rooms', title: 'Rooms' },
  { value: 'subjects', title: 'Subjects' },
  { value: 'lessons', title: 'Lessons' },
]
</script>

<template>
  <LayoutPageShell>
    <LayoutPageHeader
      title="School data"
      description="Edit the browser workspace and send the same model to the solver."
    >
      <template #actions>
        <DataWorkspaceToolbar v-model:error="importError" />
      </template>
    </LayoutPageHeader>

    <v-alert
      v-if="importError"
      type="error"
      class="mb-4"
      closable
      @click:close="importError = ''"
    >
      {{ importError }}
    </v-alert>

    <v-tabs
      v-model="tab"
      show-arrows
      class="mb-4"
    >
      <v-tab
        v-for="item in tabs"
        :key="item.value"
        :value="item.value"
      >
        {{ item.title }}
      </v-tab>
    </v-tabs>

    <v-window v-model="tab">
      <v-window-item value="calendar">
        <DataCalendarEditor />
      </v-window-item>
      <v-window-item value="years">
        <DataEntityTable kind="year" />
      </v-window-item>
      <v-window-item value="divisions">
        <DataEntityTable kind="division" />
      </v-window-item>
      <v-window-item value="groups">
        <DataEntityTable kind="group" />
      </v-window-item>
      <v-window-item value="teachers">
        <DataEntityTable kind="teacher" />
      </v-window-item>
      <v-window-item value="rooms">
        <DataEntityTable kind="room" />
      </v-window-item>
      <v-window-item value="subjects">
        <LayoutPageSection title="Subjects">
          <DataEntityTable kind="subject" />
        </LayoutPageSection>
        <LayoutPageSection title="Room rules">
          <DataSubjectRoomRules />
        </LayoutPageSection>
      </v-window-item>
      <v-window-item value="lessons">
        <DataEntityTable kind="lesson" />
      </v-window-item>
    </v-window>
  </LayoutPageShell>
</template>
