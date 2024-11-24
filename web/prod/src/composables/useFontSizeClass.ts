import { computed, Ref } from 'vue';

export function useFontSizeClass(text: Ref<string> | string) {
	const fontSizeClass = computed(() => {
		const textValue = typeof text === 'string' ? text : text.value;

		if (textValue.length <= 3) {
			return 'font-large';
		} else if (textValue.length <= 5) {
			return 'font-medium';
		} else if (textValue.length <= 7) {
			return 'font-small';
		} else {
			return 'font-extra-small';
		}
	});

	return {
		fontSizeClass,
	};
}
