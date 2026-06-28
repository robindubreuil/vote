package server

const loginPageHTML = `<!DOCTYPE html>
<html lang="fr">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<title>Tableau de bord — connexion</title>
<style>
:root { color-scheme: dark; }
* { box-sizing: border-box; }
body { margin:0; min-height:100vh; display:flex; align-items:center; justify-content:center;
  background:#0f1115; color:#e6e6e6; font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif; }
.card { background:#1a1d24; border:1px solid #2a2f3a; border-radius:12px; padding:2rem; width:100%; max-width:360px; }
h1 { margin:0 0 .25rem; font-size:1.25rem; font-weight:600; }
p.sub { margin:0 0 1.5rem; color:#8a93a6; font-size:.875rem; }
label { display:block; margin-bottom:.5rem; font-size:.8rem; color:#8a93a6; }
input[type=password] { width:100%; padding:.7rem .8rem; border-radius:8px; border:1px solid #2a2f3a;
  background:#0f1115; color:#e6e6e6; font-size:1rem; }
input[type=password]:focus { outline:none; border-color:#4a7cff; }
button { margin-top:1rem; width:100%; padding:.75rem; border:0; border-radius:8px; cursor:pointer;
  background:#4a7cff; color:#fff; font-size:1rem; font-weight:600; }
button:hover { background:#3a6cef; }
.error { margin-top:1rem; padding:.6rem .8rem; background:#3a1518; border:1px solid #5a2128;
  border-radius:8px; color:#ff8a8a; font-size:.85rem; }
</style>
</head>
<body>
<form class="card" method="POST" action="/dashboard/login" autocomplete="off">
  <h1>Tableau de bord</h1>
  <p class="sub">Accès réservé au responsable du service.</p>
  <label for="password">Secret</label>
  <input id="password" name="password" type="password" autofocus required>
  <button type="submit">Se connecter</button>
</form>
</body>
</html>`

const loginFailedHTML = `<!DOCTYPE html>
<html lang="fr">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<title>Tableau de bord — connexion</title>
<style>
:root { color-scheme: dark; }
* { box-sizing: border-box; }
body { margin:0; min-height:100vh; display:flex; align-items:center; justify-content:center;
  background:#0f1115; color:#e6e6e6; font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif; }
.card { background:#1a1d24; border:1px solid #2a2f3a; border-radius:12px; padding:2rem; width:100%; max-width:360px; }
h1 { margin:0 0 .25rem; font-size:1.25rem; font-weight:600; }
p.sub { margin:0 0 1.5rem; color:#8a93a6; font-size:.875rem; }
label { display:block; margin-bottom:.5rem; font-size:.8rem; color:#8a93a6; }
input[type=password] { width:100%; padding:.7rem .8rem; border-radius:8px; border:1px solid #2a2f3a;
  background:#0f1115; color:#e6e6e6; font-size:1rem; }
input[type=password]:focus { outline:none; border-color:#4a7cff; }
button { margin-top:1rem; width:100%; padding:.75rem; border:0; border-radius:8px; cursor:pointer;
  background:#4a7cff; color:#fff; font-size:1rem; font-weight:600; }
button:hover { background:#3a6cef; }
.error { margin:1rem 0 0; padding:.6rem .8rem; background:#3a1518; border:1px solid #5a2128;
  border-radius:8px; color:#ff8a8a; font-size:.85rem; }
</style>
</head>
<body>
<form class="card" method="POST" action="/dashboard/login" autocomplete="off">
  <h1>Tableau de bord</h1>
  <p class="sub">Accès réservé au responsable du service.</p>
  <label for="password">Secret</label>
  <input id="password" name="password" type="password" autofocus required>
  <button type="submit">Se connecter</button>
  <div class="error">Secret incorrect.</div>
</form>
</body>
</html>`
