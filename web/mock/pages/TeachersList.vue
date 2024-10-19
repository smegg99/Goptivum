<template>
  <table>
    <tbody>
      <tr>
        <td>
            <a href="n7.html"><button>Chicken Wings Tyrone</button></a>
            <a href="n10.html"><button>Michael Cheese-Hairy</button></a>
            <a href="n11.html"><button>Watermelon Malik Robbinson</button></a>
            <a href="n6.html"><button>Sheeeit Aaliyah Johnson</button></a>
            <a href="n1.html"><button>Airno Goerge-Yeet</button></a>
            <a href="n12.html"><button>Chicken Wings Kiara</button></a>
            <a href="n14.html"><button>Watermelon Jayden Lee</button></a>
            <a href="n15.html"><button>Phil McCrackin</button></a>
            <a href="n91.html"><button>Sheeeit Isaiah Brown</button></a>
            <a href="n16.html"><button>Watermelon Maya Wilson</button></a>
            <a href="n17.html"><button>Chicken Wings Zion Taylor</button></a>
            <a href="n18.html"><button>McElijah Moore</button></a>
            <a href="n19.html"><button>Sheeeit George Anderson</button></a>
            <a href="n2.html"><button>McMalik Thomas</button></a>
            <a href="n20.html"><button>Chicken Wings Aaliyah</button></a>
            <a href="n72.html"><button>Watermelon Jaden Garcia</button></a>
            <a href="n90.html"><button>Cracker Martinez</button></a>
        </td>
      </tr>
    </tbody>
  </table>
</template>

<style>
body {
	color: #000000;
	font: 11px verdana, sans-serif;
	margin: 0;
	text-align: center;
	line-height: 1em;
	padding-top: 55px;
}

h1 {
	background-color: #80C2AB;
	color: #ffffff;
	height: 50px;
	font: bold 36px verdana, sans-serif;
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
