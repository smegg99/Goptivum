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
    background: '#16171E', // '#1A1C23',
    surface: '#121218',
    primary: '#6fa8b4',
    primaryMuted: '#6EA8B5',
    secondary: '#AD487C',
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
    scheduleBorder: '#343746',
    scheduleLink: '#8BE9FD',
    scheduleLinkAlt: '#FF79C6',
    scheduleCurrentLesson: '#DB4343',
    scheduleCurrentBreak: '#36D05F',
  },
}

const lightTheme = {
  dark: false,
  colors: {
    background: '#EBEBEB',
    surface: '#F4F4F6',
    primary: '#556BDD',
    primaryMuted: '#4D8DE0',
    secondary: '#BD6FDE',
    secondaryMuted: '#A570E9',
    accent: '#7C3EAE',
    error: '#E53935',
    info: '#1976D2',
    success: '#43A047',
    warning: '#FBC02D',
    foreground: '#121212',
    comment: '#6D6D6D',
    textPrimary: '#1C1C1C',
    textSecondary: '#005FCC',
    textMuted: '#7A7A7A',
    gradient1: '#B22FEA',
    gradient2: '#4D8DE0',
    scheduleBorder: '#ADADAD',
    scheduleLink: '#415AD9',
    scheduleLinkAlt: '#B257D9',
    scheduleCurrentLesson: '#DB4343',
    scheduleCurrentBreak: '#36D05F',
  },
};

// Export the Vuetify configuration
export default createVuetify({
  theme: {
    defaultTheme: 'light',
    themes: {
      light: lightTheme,
      dracula: draculaTheme,
    },
  },
})
