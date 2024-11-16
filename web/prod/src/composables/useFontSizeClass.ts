import { computed } from 'vue'

export function useFontSizeClass(text: string) {
	const fontSizeClass = computed(() => {
		if (text.length >= 20) return 'font-smaller'
		if (text.length >= 15) return 'font-small'
		if (text.length >= 10) return 'font-medium'
		if (text.length >= 5) return 'font-large'

		return 'font-larger'
	})

	return {
		fontSizeClass,
	}
}