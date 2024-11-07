<!-- App.vue -->
<template>
  <v-app>
    <Overlay />
  </v-app>
</template>

<style>
@import url('https://fonts.googleapis.com/css2?family=Overpass:wght@300;400;600;700&display=swap');
@import url('https://fonts.googleapis.com/css2?family=Material+Icons+Outlined');

html,
body,
.v-application {
  font-family: 'Overpass', sans-serif; 
}

.text-gradient-yellow-pink {
  background: linear-gradient(90deg, #f9e79f, #f1948a);
  background-clip: text;
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}

.text-gradient-green {
  background: linear-gradient(90deg, #50fa7b, #3ff57c);
  background-clip: text;
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}

.text-gradient-blue-purple {
  background: linear-gradient(90deg, #8be9fd, #6272a4);
  background-clip: text;
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}
</style>