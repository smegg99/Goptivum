// app/theme/defaults.ts

import type { DefaultsInstance } from 'vuetify'

export const defaults: DefaultsInstance = {
  VCard: { variant: 'flat', rounded: 'lg', border: true },
  VBtn: { variant: 'flat', rounded: 'lg' },
  VChip: { rounded: 'lg', size: 'small' },
  VTextField: { variant: 'outlined', density: 'compact', hideDetails: 'auto' },
  VSelect: { variant: 'outlined', density: 'compact', hideDetails: 'auto' },
  VDataTable: { density: 'compact' },
  VDialog: { maxWidth: 560 },
}
