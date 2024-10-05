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
	overflow: visible;
	user-select: none;
	overflow: visible;
	transform-origin: center;
	display: flex;
}

.digit-group {
	position: relative;
	width: 100%;
	height: 100%;
	overflow: hidden;
}

.digit-flip {
	font-size: 12vw;
	font-weight: bold;
	color: #f7fafc;
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

.flip-enter-active,
.flip-leave-active {
	transition: transform 1s ease, opacity 1s ease;
}

.flip-enter-from {
	transform: translateY(100%);
}

.flip-enter-to {
	transform: translateY(0);
}

.flip-leave-to {
	transform: translateY(-100%);
}

</style>