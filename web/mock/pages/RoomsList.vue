<template>
  <div class="buttons-container">
    <a href="https://zsem.edu.pl/plany/plany/s2.html"><button>4</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s1.html"><button>5</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s4.html"><button>6</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s5.html"><button>7</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s3.html"><button>8</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s6.html"><button>11</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s35.html"><button>s.gimn. 1</button></a> &nbsp;
    <br><br>
    <a href="https://zsem.edu.pl/plany/plany/s7.html"><button>12</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s8.html"><button>13</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s9.html"><button>14</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s10.html"><button>15</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s27.html"><button>16</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s11.html"><button>38</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s36.html"><button>s.gimn. 2</button></a> &nbsp;
    <br><br>
    <a href="https://zsem.edu.pl/plany/plany/s12.html"><button>39</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s16.html"><button>40</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s17.html"><button>41</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s18.html"><button>42</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s19.html"><button>43</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s20.html"><button>44</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s37.html"><button>s.gimn. 3</button></a> &nbsp;
    <br><br>
    <a href="https://zsem.edu.pl/plany/plany/s21.html"><button>45</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s22.html"><button>46</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s23.html"><button>47</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s24.html"><button>48</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s25.html"><button>51</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s26.html"><button>52</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s38.html"><button>s.gimn. 4</button></a> &nbsp;
    <br><br>

    <a href="https://zsem.edu.pl/plany/plany/s28.html"><button>s.język. 1</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s29.html"><button>s.język. 2</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s30.html"><button>s.język. 3</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s31.html"><button>s.język. 4</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s32.html"><button>s.język. 5</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s33.html"><button>s.język. 6</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s34.html"><button>s.język. 7</button></a> &nbsp;
    <br><br>
    <a href="https://zsem.edu.pl/plany/plany/s40.html"><button>prac. 3</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s41.html"><button>prac. 4</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s42.html"><button>pr.autom.</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s39.html"><button>prac. 106</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s13.html"><button>prac. 107</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s14.html"><button>prac. 108</button></a> &nbsp;
    <a href="https://zsem.edu.pl/plany/plany/s15.html"><button>prac. 109</button></a> &nbsp;
  </div>
</template>

<script>
export default {
  name: 'RoomList'
}
</script>

<style scoped>
.buttons-container {
	text-align: center;
	line-height: 1em;
}

.buttons-container button {
	margin: 0;
	width: 125px;
	font-size: 20px;
	height: 50px;
}

.buttons-container a {
	text-decoration: none;
	color: black;
}

.buttons-container a:active button {
	background: yellow;
}

.buttons-container a[href]>button:hover {
	cursor: pointer !important;
}
</style>
