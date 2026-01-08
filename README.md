# Vote Coloré

Application de vote en temps réel pour formations - Backend Go + Frontends Vanilla JS.

## Démarrage rapide

```bash
# Backend (port 8080)
make run

# Formateur (port 5173)
cd frontend-formateur && npm run dev

# Stagiaire (port 5174)
cd frontend-stagiaire && npm run dev
```

## Fonctionnalités

- **Formateur** : génération de code, sélection couleurs, choix multiple, timer, stats temps réel
- **Stagiaire** : connexion par code, vote simple/multiple, reconnexion automatique
- **Temps réel** : WebSocket pour communication instantanée
- **Persistance** : noms et configuration conservés entre sessions

## Structure

```
vote/
├── backend/              # Serveur Go WebSocket
├── frontend-formateur/   # Interface formateur
├── frontend-stagiaire/   # Interface stagiaire
├── shared/               # Code partagé
└── scripts/              # Build automation
```

## Build prod

```bash
make build                                    # Backend
cd frontend-formateur && npm run build       # Formateur
cd frontend-stagiaire && npm run build       # Stagiaire
```

## Environment

Variables d'environnement optionnelles (voir `.env.example`) :

- `PORT` : Port du serveur (défaut: 8080)
- `ALLOWED_ORIGINS` : Origines CORS autorisées (défaut: *)

## Tests

```bash
make test              # Backend tests
npm test               # Frontend tests (Vitest)
```

## License

MIT
