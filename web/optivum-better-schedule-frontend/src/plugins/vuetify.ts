/**
 * plugins/vuetify.ts
 *
 * Framework documentation: https://vuetifyjs.com
 */

// Styles
import '@mdi/font/css/materialdesignicons.css'
import 'vuetify/styles'

// Composables
import { createVuetify } from 'vuetify'

const draculaTheme = {
  dark: true,
  colors: {
    background: '#1A1C23',
    surface: '#121218',
    primary: '#8BE9FD',
    primaryMuted: '#6EA8B5',
    secondary: '#FF79C6',
    secondaryMuted: '#8A3B64',
    accent: '#BD93F9',
    error: '#FF5555',
    info: '#8BE9FD',
    success: '#50FA7B',
    warning: '#F1FA8C',
    foreground: '#F8F8F2',
    comment: '#6272A4',
    textPrimary: '#E0E0E0',
    textSecondary: '#4A90E2',
    textMuted: '#A8A8A8',
    gradient1: '#8A3B64',
    gradient2: '#6EA8B5',
  },
}

const lightTheme = {
  dark: false,
  colors: {
    background: '#FFFFFF',
    surface: '#FAFAFA',
    primary: '#0079C9',
    primaryMuted: '#688FDF',
    secondary: '#A91AD9',
    secondaryMuted: '#9F67E0',
    accent: '#512DA8',
    error: '#C62828',
    info: '#1565C0',
    success: '#2E7D32',
    warning: '#F9A825',
    foreground: '#212121',
    comment: '#616161',
    textPrimary: '#212121',
    textSecondary: '#0079C9',
    textMuted: '#9E9E9E',
    gradient1: '#9F67E0',
    gradient2: '#688FDF',
  },
}

const darkTheme = {
  dark: true,
  colors: {
    background: '#121212',
    surface: '#1E1E1E',
    primary: '#4A90E2',
    primaryMuted: '#5094E2',
    secondary: '#FF8C00',
    secondaryMuted: '#FFAB40',
    accent: '#03DAC6',
    error: '#E57373',
    info: '#42A5F5',
    success: '#81C784',
    warning: '#FFCA28',
    foreground: '#E0E0E0',
    comment: '#B0BEC5',
    textPrimary: '#E0E0E0',
    textSecondary: '#4A90E2',
    textMuted: '#A8A8A8',
    gradient1: '#5094E2',
    gradient2: '#FFAB40',
  },
}

const oledTheme = {
  dark: true,
  colors: {
    background: '#000000',
    surface: '#000000',
    primary: '#1E90FF',
    primaryMuted: '#0A0A0A',
    secondary: '#FF4500',
    secondaryMuted: '#0A0A0A',
    accent: '#00CED1',
    error: '#FF6347',
    info: '#1E90FF',
    success: '#32CD32',
    warning: '#FFD700',
    foreground: '#E0E0E0',
    comment: '#A9A9A9',
    textPrimary: '#E0E0E0',
    textSecondary: '#1E90FF',
    textMuted: '#A9A9A9',
    gradient1: '#E0E0E0',
    gradient2: '#E0E0E0',
  },
}

// Export the Vuetify configuration
export default createVuetify({
  theme: {
    defaultTheme: 'light',
    themes: {
      light: lightTheme,
      dark: darkTheme,
      dracula: draculaTheme,
      oled: oledTheme,
    },
  },
})
