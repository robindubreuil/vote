# Vote Coloré - Application de vote temps réel

Backend Go (WebSocket) + Frontends Vanilla JS (Vite)

## Démarrage rapide

```bash
# Backend (port 8080)
cd backend && ./vote-server  # ou: go run .

# Formateur (port 5173)
cd frontend-formateur && npm run dev

# Stagiaire (port 5174)
cd frontend-stagiaire && npm run dev
```

## Structure

```
vote/
├── backend/
│   ├── main.go       # Serveur Gin, endpoint /ws
│   ├── hub.go        # Hub WebSocket, SessionState
│   └── handler.go    # readPump/writePump, handle*
├── frontend-formateur/src/
│   ├── main.js       # Config, vote, stats temps réel
│   └── style.css
└── frontend-stagiaire/src/
    ├── main.js       # Join code, vote, reconnexion auto
    └── style.css
```

## Protocole WebSocket (ws://localhost:8080/ws)

**Client → Serveur:**
```json
{"type":"trainer_join","sessionCode":"8472","trainerId":"..."}
{"type":"stagiaire_join","sessionCode":"8472","stagiaireId":"..."}
{"type":"start_vote","colors":["rouge","vert","bleu"],"multipleChoice":false}
{"type":"vote","couleurs":["rouge"],"stagiaireId":"..."}
{"type":"close_vote"}
{"type":"reset_vote","colors":[...],"multipleChoice":...}
```

**Serveur → Formateur:**
```json
{"type":"session_created","sessionCode":"8472"}
{"type":"connected_count","count":12}
{"type":"vote_received","stagiaireId":"...","couleurs":["rouge"]}
```

**Serveur → Stagiaire:**
```json
{"type":"session_joined","sessionCode":"8472"}
{"type":"join_error"}
{"type":"vote_started","colors":[...],"multipleChoice":false}
{"type":"vote_accepted"}
{"type":"vote_closed"}
{"type":"vote_reset"}
```

## Fonctionnalités à tester

1. **Formateur** - Génération code, config couleurs, choix multiple toggle
2. **Stagiaire** - Join par code, vote simple/multiple, reconnexion auto
3. **Temps réel** - Votes arrivent instantanément, timer fonctionnel
4. **Stats** - Par couleur ET par combinaison (si multiple)
5. **Persistance config** - "Nouveau vote" garde la config
6. **Reconnexion** - Formateur reconnecte, stagiaires restent connectés

## Couleurs disponibles

rouge (#ef4444), vert (#22c55e), bleu (#3b82f6), jaune (#eab308),
orange (#f97316), violet (#a855f7), rose (#ec4899), gris (#6b7280)

## Build prod

```bash
# Backend
cd backend && go build -o vote-server .

# Frontends
cd frontend-formateur && npm run build
cd frontend-stagiaire && npm run build
```
