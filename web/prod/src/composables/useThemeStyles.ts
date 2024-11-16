import { computed } from 'vue';
import { useTheme } from 'vuetify';

export function useTextGradientClass(variant: string) {
	const theme = useTheme();
	return computed(() => `gradient-${theme.global.name.value}-text-${variant}`);
}

export function useBackgroundGradientClass(variant: string) {
	const theme = useTheme();
	return computed(() => `gradient-${theme.global.name.value}-bg-${variant}`);
}

export function useAirQualityTextClass(variant: string, themeName: string) {
	return computed(() => `gradient-${themeName}-text-air-${variant}`);
}