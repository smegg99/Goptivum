<!-- Overlay.vue -->
<template>
	<v-navigation-drawer v-model="drawer" class="elevation-8">
		<template #prepend>
			<v-slide-x-transition appear>
				<v-list nav density="default">
					<v-list-item v-for="item in items" :key="item.route" :to="item.route" nav link
						class="ma-2 nav-item overflow-visible" rounded="xl" @dragstart.prevent @mousedown.stop
						draggable="false">
						<template #prepend>
							<v-icon>{{ item.prependIcon }}</v-icon>
						</template>
						<template #title>
							<!-- <span :class="['nav-item-title', textGradPrimaryAccent]">{{ item.title }}</span> -->
							<span class="nav-item-title">{{ item.title }}</span>
						</template>
					</v-list-item>
				</v-list>
			</v-slide-x-transition>
		</template>

		<template #append>
			<v-slide-y-reverse-transition appear>
				<v-list nav density="default">
					<v-list-item class="ma-3 nav-item" nav link :to="'/settings'" rounded="xl" @dragstart.prevent
						@mousedown.stop draggable="false">
						<template #prepend>
							<v-icon>mdi-cog-outline</v-icon>
						</template>
						<template #title>
							<span class="nav-item-title">{{ t('page.settings') }}</span>
						</template>
					</v-list-item>
				</v-list>
			</v-slide-y-reverse-transition>
		</template>
	</v-navigation-drawer>

	<v-slide-x-transition appear>
		<v-btn icon="mdi-menu" elevation="8" class="fab rounded-pill" @click="drawer = !drawer" @dragstart.prevent
			@mousedown.stop draggable="false" />
	</v-slide-x-transition>

	<v-main>
		<div class="background-container">
			<router-view>
				<template v-slot="{ Component }">
					<component :is="Component" />
				</template>
			</router-view>
		</div>
	</v-main>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue';
import { useI18n } from 'vue-i18n';

const { t } = useI18n();
const drawer = ref(true);

const items = computed(() => [
	{
		title: t('page.home'),
		prependIcon: 'mdi-view-dashboard-outline',
		route: '/',
	},
	{
		title: t('page.divisions'),
		prependIcon: 'mdi-school-outline',
		route: '/divisions',
	},
	{
		title: t('page.teachers'),
		prependIcon: 'mdi-human-male-board',
		route: '/teachers',
	},
	{
		title: t('page.rooms'),
		prependIcon: 'mdi-door',
		route: '/rooms',
	},
	{
		title: t('page.duties'),
		prependIcon: 'mdi-shield-star-outline',
		route: '/duties',
	},
	{
		title: t('page.practices'),
		prependIcon: 'mdi-hammer-wrench',
		route: '/practices',
	},
	// {
	// 	title: t('page.map'),
	// 	prependIcon: 'mdi-map-outline',
	// 	route: '/map',
	// },
]);
</script>

<style scoped>
:deep(.v-list-item-title) {
	overflow: visible !important;
	white-space: normal;
	word-wrap: break-word;
}

:deep(.v-list-item__content) {
	overflow: visible !important;
	white-space: normal;
	word-wrap: break-word;
}

.background-container {
	height: 100%;
	padding: 0;
	overflow: hidden;
}

.nav-item :deep(.v-list-item-title) {
	font-size: 1.25rem;
	height: 1.5rem;
}

.nav-item :deep(.v-icon) {
	font-size: 1.5rem;
}

.nav-item-title {
	user-select: none;
	overflow: visible !important;
	text-align: left;
}

.nav-item :deep(.v-list-item__content) {
	display: flex;
	align-items: center;
	justify-content: flex-start;
	flex-direction: row;
	align-items: center;
	text-align: left;
	overflow: visible !important;
}

.nav-item :deep(.v-list-item-title) {
	display: flex;
	align-items: center;
	justify-content: center;
	flex-direction: row;
	text-align: center;
	align-items: center;
	overflow: visible;
}

.v-list-item--nav {
	padding-inline: 16px;
}

.no-scroll {
	overflow: hidden !important;
}

.grid-container {
	display: grid;
	grid-template-columns: 1fr 1fr;
	align-items: center;
	justify-items: center;
	max-height: 100%;
}

.fab {
	width: 64px !important;
	height: 64px !important;
	top: 16px;
	left: 16px;
	display: flex;
	align-items: center;
	justify-content: center;
	position: fixed;
	z-index: 999;
}

.v-btn {
	margin: 0;
	padding: 0;
}

.v-btn>.v-icon {
	font-size: 24px;
}

@media screen and (max-width: 767px) {
	.nav-item :deep(.v-list-item-title) {
		font-size: 0.9rem;
	}

	.nav-item :deep(.v-icon) {
		font-size: 1.25rem;
	}
}
</style>
