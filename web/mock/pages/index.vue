<template>
  <div>
    <nav>
      <router-link to="/">
        Home
      </router-link> |
      <router-link to="/n1.html">
        Teacher
      </router-link> |
      <router-link to="/nnn.php">
        Teachers List
      </router-link> |
      <router-link to="/o1.html">
        Division
      </router-link> |
      <router-link to="/lll.php">
        Divisions List
      </router-link> |
      <router-link to="/s1.html">
        Room
      </router-link> |
      <router-link to="/sss.php">
        Rooms List
      </router-link> |
    </nav>
    <router-view />
  </div>
</template>

<style>
#app {
  font-family: Avenir, Helvetica, Arial, sans-serif;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
  text-align: center;
  color: #2c3e50;
}

nav {
  padding: 30px;
}

nav a {
  font-weight: bold;
  color: #2c3e50;
}

nav a.router-link-exact-active {
  color: #42b983;
}
</style>
