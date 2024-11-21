<!-- App.vue -->
<template>
  <v-app>
    <Overlay />
  </v-app>
</template>

<script setup lang="ts">

</script>

<style lang="scss">
@import url('https://fonts.googleapis.com/css2?family=Overpass:wght@300;400;600;700&display=swap');
@import url('https://fonts.googleapis.com/css2?family=Material+Icons+Outlined');

// /* overpass-200 - cyrillic_cyrillic-ext_latin_latin-ext */
// @font-face {
//   font-display: swap;
//   /* Check https://developer.mozilla.org/en-US/docs/Web/CSS/@font-face/font-display for other options. */
//   font-family: 'Overpass';
//   font-style: normal;
//   font-weight: 200;
//   src: url('@/assets/fonts/overpass-v16-cyrillic_cyrillic-ext_latin_latin-ext-200.woff2') format('woff2');
//   /* Chrome 36+, Opera 23+, Firefox 39+, Safari 12+, iOS 10+ */
// }

// /* overpass-300 - cyrillic_cyrillic-ext_latin_latin-ext */
// @font-face {
//   font-display: swap;
//   /* Check https://developer.mozilla.org/en-US/docs/Web/CSS/@font-face/font-display for other options. */
//   font-family: 'Overpass';
//   font-style: normal;
//   font-weight: 300;
//   src: url('@/assets/fonts/overpass-v16-cyrillic_cyrillic-ext_latin_latin-ext-300.woff2') format('woff2');
//   /* Chrome 36+, Opera 23+, Firefox 39+, Safari 12+, iOS 10+ */
// }

// /* overpass-regular - cyrillic_cyrillic-ext_latin_latin-ext */
// @font-face {
//   font-display: swap;
//   /* Check https://developer.mozilla.org/en-US/docs/Web/CSS/@font-face/font-display for other options. */
//   font-family: 'Overpass';
//   font-style: normal;
//   font-weight: 400;
//   src: url('@/assets/fonts/overpass-v16-cyrillic_cyrillic-ext_latin_latin-ext-regular.woff2') format('woff2');
//   /* Chrome 36+, Opera 23+, Firefox 39+, Safari 12+, iOS 10+ */
// }

// /* overpass-500 - cyrillic_cyrillic-ext_latin_latin-ext */
// @font-face {
//   font-display: swap;
//   /* Check https://developer.mozilla.org/en-US/docs/Web/CSS/@font-face/font-display for other options. */
//   font-family: 'Overpass';
//   font-style: normal;
//   font-weight: 500;
//   src: url('@/assets/fonts/overpass-v16-cyrillic_cyrillic-ext_latin_latin-ext-500.woff2') format('woff2');
//   /* Chrome 36+, Opera 23+, Firefox 39+, Safari 12+, iOS 10+ */
// }

// /* overpass-600 - cyrillic_cyrillic-ext_latin_latin-ext */
// @font-face {
//   font-display: swap;
//   /* Check https://developer.mozilla.org/en-US/docs/Web/CSS/@font-face/font-display for other options. */
//   font-family: 'Overpass';
//   font-style: normal;
//   font-weight: 600;
//   src: url('@/assets/fonts/overpass-v16-cyrillic_cyrillic-ext_latin_latin-ext-600.woff2') format('woff2');
//   /* Chrome 36+, Opera 23+, Firefox 39+, Safari 12+, iOS 10+ */
// }

// /* overpass-700 - cyrillic_cyrillic-ext_latin_latin-ext */
// @font-face {
//   font-display: swap;
//   /* Check https://developer.mozilla.org/en-US/docs/Web/CSS/@font-face/font-display for other options. */
//   font-family: 'Overpass';
//   font-style: normal;
//   font-weight: 700;
//   src: url('@/assets/fonts/overpass-v16-cyrillic_cyrillic-ext_latin_latin-ext-700.woff2') format('woff2');
//   /* Chrome 36+, Opera 23+, Firefox 39+, Safari 12+, iOS 10+ */
// }

// /* overpass-800 - cyrillic_cyrillic-ext_latin_latin-ext */
// @font-face {
//   font-display: swap;
//   /* Check https://developer.mozilla.org/en-US/docs/Web/CSS/@font-face/font-display for other options. */
//   font-family: 'Overpass';
//   font-style: normal;
//   font-weight: 800;
//   src: url('@/assets/fonts/overpass-v16-cyrillic_cyrillic-ext_latin_latin-ext-800.woff2') format('woff2');
//   /* Chrome 36+, Opera 23+, Firefox 39+, Safari 12+, iOS 10+ */
// }

// /* overpass-900 - cyrillic_cyrillic-ext_latin_latin-ext */
// @font-face {
//   font-display: swap;
//   /* Check https://developer.mozilla.org/en-US/docs/Web/CSS/@font-face/font-display for other options. */
//   font-family: 'Overpass';
//   font-style: normal;
//   font-weight: 900;
//   src: url('@/assets/fonts/overpass-v16-cyrillic_cyrillic-ext_latin_latin-ext-900.woff2') format('woff2');
//   /* Chrome 36+, Opera 23+, Firefox 39+, Safari 12+, iOS 10+ */
// }

html,
body,
.v-application {
  font-family: 'Overpass', sans-serif; 
}
</style>