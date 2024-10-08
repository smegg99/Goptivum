<!-- Digit.vue -->
<template>
	<div class="digit">
		<transition-group name="flip" tag="div" mode="out-in" class="digit-group">
			<div v-for="(char, index) in value" :key="`${char}-${index}`" class="digit-flip">
				{{ char }}
			</div>
		</transition-group>
	</div>
</template>

<script lang="ts" setup>
const { value } = defineProps({
	value: {
		type: String,
		required: true,
	}
});
</script>

<style scoped>
.digit {
	display: flex;
	align-items: center;
	justify-content: center;
	width: 8vw;
	height: 12vw;
	user-select: none;
	display: flex;
}

.digit-group {
	position: relative;
	display: flex;
	align-items: center;
	justify-content: center;
	width: 100%;
	height: 100%;
	margin-top: 2vw;
	overflow: visible;
}

.digit-flip {
	font-size: 12vw;
	font-weight: bold;
	transition: transform 1s ease, opacity 1s ease;
	transform-origin: center;
	position: absolute;
	width: 100%;
	height: 100%;
	display: flex;
	align-items: center;
	justify-content: center;
	overflow: hidden;
}

.flip-enter-active {
	transition: transform 0.45s ease, opacity 0.45s ease;
}

.flip-leave-active {
	transition: transform 0.75s ease, opacity 0.75s ease;
}

.flip-enter-from {
	transform: translateY(-140%);
}

.flip-enter-to {
	transform: translateY(10%);
}

.flip-leave-to {
	transform: translateY(140%);
}
</style>