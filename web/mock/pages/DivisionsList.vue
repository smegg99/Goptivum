<template>
  <div>
    <a id="top" />

    <table>
      <tbody>
        <tr>
          <td>
            <a class="hidden"><button /></a><br>
            <a href="https://zsem.edu.pl/plany/plany/o30.html" target="plan"><button>1I</button></a><br>
            <a href="https://zsem.edu.pl/plany/plany/o29.html" target="plan"><button>1D</button></a><br>
            <a href="https://zsem.edu.pl/plany/plany/o28.html" target="plan"><button>1P</button></a><br>
            <a href="https://zsem.edu.pl/plany/plany/o32.html" target="plan"><button>1F</button></a><br>
            <a href="https://zsem.edu.pl/plany/plany/o18.html" target="plan">
              <button>3E<br><span style="font-size:12px">prakt.</span></button>
            </a><br>
            <a href="https://zsem.edu.pl/plany/plany/o31.html" target="plan"><button>1M</button></a><br>
          </td>
          <td>
            <a href="https://zsem.edu.pl/plany/plany/o27.html" target="plan"><button>2T</button></a><br>
            <a href="https://zsem.edu.pl/plany/plany/o24.html" target="plan"><button>2I</button></a><br>
            <a href="https://zsem.edu.pl/plany/plany/o21.html" target="plan"><button>2D</button></a><br>
            <a href="https://zsem.edu.pl/plany/plany/o22.html" target="plan"><button>2P</button></a><br>
            <a href="https://zsem.edu.pl/plany/plany/o26.html" target="plan"><button>2F</button></a><br>
            <a href="https://zsem.edu.pl/plany/plany/o25.html" target="plan"><button>2G</button></a><br>
            <a href="https://zsem.edu.pl/plany/plany/o1.html" target="plan"><button>5M</button></a><br>
          </td>
        </tr>
      </tbody>
    </table>

    <div>
      <h2><a href="https://zsem.edu.pl/plany/nnn.php" target="plan"><button>Nauczyciele</button></a></h2>
      <h2><a href="https://zsem.edu.pl/plany/sss.php" target="plan"><button>Sale</button></a></h2>
      <h2><a href="https://zsem.edu.pl/wolne-sale" target="plan"><button>Wolne Sale</button></a></h2>
      <h2><a href="https://zsem.edu.pl/plany/dyzury.php" target="plan"><button>Dyżury</button></a></h2>
      <h2><a href="https://zsem.edu.pl/plany/praktyki_lista.php" target="plan"><button>Praktyki</button></a></h2>
    </div>
  </div>
</template>

<style scoped>
body {
	color: #000000;
	font: 11px Verdana, sans-serif;
	margin: 0;
	text-align: center;
	line-height: 1em;
	padding-top: 55px;
}

h1 {
	background-color: #80C2AB;
	color: #ffffff;
	height: 50px;
	font: bold 36px Verdana, sans-serif;
	border-bottom: 1px solid black;
	margin: 0;
	position: fixed;
	top: 0;
	width: 100%;
}

button {
	margin: 2px;
	width: 170px;
	font-size: 16px;
	height: 43px;
	border-style: solid;
	font-weight: bold;
	background-color: #fff;
}

a {
	text-decoration: none;
}

a:link,
a:visited {
	color: black;
}

a.empty {
	pointer-events: none;
}

a[href]>button:hover {
	cursor: pointer !important;
}

table {
	text-align: center;
	margin: auto;
	border-collapse: collapse;
}

td {
	vertical-align: top;
}

td:nth-child(2n+1) a:nth-of-type(2n+1) button,
td:nth-child(2n) a:nth-of-type(2n) button {
	background-color: #bbb;
}

a:active button {
	background: yellow !important;
}
</style>
