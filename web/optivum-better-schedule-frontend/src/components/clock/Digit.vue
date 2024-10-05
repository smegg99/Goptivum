<template>
	<div class="digit-container">
		<div class="digit">
			<transition-group name="flip" tag="div" mode="out-in">
				<div v-for="(char, index) in value" :key="`${char}-${index}`" class="digit-flip">
					{{ char }}
				</div>
			</transition-group>
		</div>
	</div>
</template>

<script lang="ts" setup>
const { value } = defineProps({
	value: {
		type: String,
		required: true,
	},
});
</script>

<style scoped>
.digit-container {
	display: flex;
	justify-content: center;
	align-items: center;
	overflow: visible;
	user-select: none;
}

.digit {
	display: flex;
	position: relative;
	height: 5rem;
	width: 2.5rem;
	overflow: visible;
	user-select: none;
}

.digit-flip {
	font-size: 4rem;
	font-weight: bold;
	color: #f7fafc;
	line-height: 5rem;
	transition: transform 0.5s ease, opacity 0.5s ease;
	transform-origin: center;
	width: 100%;
	position: absolute;
	user-select: none;
}

.flip-enter-active,
.flip-leave-active {
	transition: transform 0.5s ease, opacity 0.5s ease;
}

.flip-enter-from {
	transform: translateY(-120%) scale(0.9);
	opacity: 0;
}

.flip-enter-to {
	transform: translateY(10%);
	opacity: 1;
}

.flip-leave-to {
	transform: translateY(100%) scale(0.9);
	opacity: 0;
}
</style>