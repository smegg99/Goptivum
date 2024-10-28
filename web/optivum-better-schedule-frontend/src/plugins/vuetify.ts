/**
 * plugins/vuetify.ts
 *
 * Framework documentation: https://vuetifyjs.com`
 */

// Styles
import '@mdi/font/css/materialdesignicons.css'
import 'vuetify/styles'

// Composables
import { createVuetify } from 'vuetify'

const draculaTheme = {
  dark: true,
  colors: {
    background: '#1A1C23',    // Darker background for main content
    surface: '#121218',       // Much darker color for the drawer
    primary: '#8BE9FD',       // Cyan
    secondary: '#FF79C6',     // Pink
    accent: '#BD93F9',        // Purple
    error: '#FF5555',         // Red
    info: '#8BE9FD',          // Cyan variant
    success: '#50FA7B',       // Green
    warning: '#F1FA8C',       // Yellow
    foreground: '#F8F8F2',    // Foreground text color
    comment: '#6272A4',       // Muted text color for comments
    drawer: '#0F0F12',        // Darker shade specifically for drawer
    orange: '#FFB86C',        // Optional orange
  },
};

// https://vuetifyjs.com/en/introduction/why-vuetify/#feature-guides
export default createVuetify({
  theme: {
    defaultTheme: 'dark',
    themes: {
      light: {
        dark: false,
      },
      dark: {
        dark: true,
      },
      dracula: draculaTheme,
    },
  },
});
