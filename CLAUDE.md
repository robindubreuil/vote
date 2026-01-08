# Vote Coloré - App de vote temps réel

Backend Go (WebSocket) + Frontends Vanilla JS (Vite)

## Quickstart

```bash
make run                           # Backend (8080)
cd frontend-formateur && npm run dev   # Formateur (5173)
cd frontend-stagiaire && npm run dev   # Stagiaire (5174)
```

## Makefile cibles

| Cible | Description |
|-------|-------------|
| `make build` | Compile le binaire Go |
| `make run` | Build + lance le serveur |
| `make test` | Tests Go (race + coverage) |
| `make build-deb` | Package Debian |
| `make clean-all` | Nettoie tous les artifacts |
| `make dev` | Hot reload avec `air` |

## Structure

```
vote/
├── backend/              # Serveur Go
│   ├── main.go           # Gin server, CORS, graceful shutdown
│   ├── hub.go            # Hub WebSocket, SessionState, cleanup
│   ├── handler.go        # readPump/writePump, routing messages
│   └── *_test.go         # Tests backend
├── frontend-formateur/   # Vue formateur
│   ├── src/main.js       # Config vote, stats temps réel
│   └── src/style.css     # Thème sombre
├── frontend-stagiaire/   # Vue stagiaire
│   ├── src/main.js       # Join code, vote, reconnexion auto
│   └── src/style.css
├── shared/               # Code commun
│   ├── icons.js          # Icônes Bootstrap dégradées
│   └── version.js        # Version générée (git tags)
├── scripts/              # Build automation
│   ├── gen-version.js    # Génère version.js
│   └── compress-assets.js # gzip + brotli assets
└── debian/               # Packaging Debian
```

## WebSocket (ws://localhost:8080/ws)

**Client → Serveur:**
```
trainer_join     {sessionCode, trainerId}
stagiaire_join   {sessionCode, stagiaireId, name?}
start_vote       {colors[], multipleChoice}
vote             {couleurs[], stagiaireId}
close_vote
reset_vote       {colors[], multipleChoice}
update_name      {stagiaireId, name}
```

**Serveur → Formateur:**
```
session_created             {sessionCode}
connected_count             {count, stagiaires[{id,name}]}
vote_received               {stagiaireId, couleurs[]}
stagiaire_names_updated     {stagiaires[{id,name}]}
error                       {message}
```

**Serveur → Stagiaire:**
```
session_joined   {sessionCode}
vote_started     {colors[], multipleChoice}
vote_accepted
vote_closed
vote_reset
error            {message}
```

## Couleurs

rouge (#ef4444), vert (#22c55e), bleu (#3b82f6), jaune (#eab308),
orange (#f97316), violet (#a855f7), rose (#ec4899), gris (#6b7280)

## Fonctionnalités

- **Formateur**: génération code 4 chiffres, sélection couleurs, toggle choix multiple, timer, stats temps réel
- **Stagiaire**: join par code, vote simple/multiple, édition prénom, reconnexion auto
- **Persistance**: noms stagiaires conservés après déconnexion, config vote conservée entre sessions
- **Stats**: par couleur + par combinaison (si choix multiple)

## Config Vite

- `frontend-formateur`: base `/formateur/`
- `frontend-stagiaire`: base `/stagiaire/`

## Tests

```bash
# Backend
make test              # Tous les tests
make test-cover        # + coverage.html

# Frontend
npm test              # Vitest
npm run test:ui       # UI Vitest
```

## Build prod

```bash
make build                     # Backend
cd frontend-formateur && npm run build   # Formateur
cd frontend-stagiaire && npm run build   # Stagiaire
```
