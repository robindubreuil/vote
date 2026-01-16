# Vote Coloré - Context Guide

## Project Overview

**Vote Coloré** is a real-time voting application designed for training sessions. It allows a trainer (formateur) to create voting sessions and trainees (stagiaires) to join and vote on various topics.

### Architecture
- **Backend**: Go (Golang) server managing WebSocket connections, session state, and message routing. Uses `Gin` for HTTP and `gorilla/websocket` (or similar native implementation) for real-time comms.
- **Frontend Formateur**: Vanilla JavaScript application (Vite) for the trainer to control votes, view real-time statistics, and manage the session.
- **Frontend Stagiaire**: Vanilla JavaScript application (Vite) for trainees to join via a 4-digit code, vote, and manage their identity.
- **Shared**: Common assets (icons, versioning logic) shared between frontends.

## Key Files & Directories

- **`backend/`**: Go source code.
    - `cmd/server/main.go`: Entry point.
    - `internal/hub`: WebSocket hub and client management.
    - `internal/vote`: Vote logic and session management.
- **`frontend-formateur/`**: Trainer UI.
    - `src/main.js`: Core logic for the trainer dashboard.
- **`frontend-stagiaire/`**: Trainee UI.
    - `src/main.js`: Core logic for the voting interface.
- **`shared/`**: Shared JavaScript resources.
- **`debian/`**: Configuration for building the Debian package.
- **`Makefile`**: Primary build automation tool.
- **`CLAUDE.md`**: Detailed reference for the WebSocket protocol and features.

## Build & Run Commands

### Backend (Go)
*Run from project root*

- **Run (Dev)**: `make run` (starts server on port 8080)
- **Run (Hot Reload)**: `make dev` (requires `air`)
- **Build**: `make build`
- **Test**: `make test` (runs race detection and coverage)
- **Lint**: `make lint` (requires `golangci-lint`)
- **Debian Package**: `make build-deb`

### Frontends (Vite)
*Run from respective directories (`frontend-formateur` / `frontend-stagiaire`)*

- **Install Deps**: `npm install`
- **Run (Dev)**: `npm run dev`
    - Formateur default: `http://localhost:5173`
    - Stagiaire default: `http://localhost:5174`
- **Build**: `npm run build`
- **Test**: `npm test` (Vitest)

## Development Conventions

### WebSocket Protocol
Refer to `CLAUDE.md` for the authoritative schema of WebSocket messages (e.g., `trainer_join`, `vote`, `session_created`).

### Styling & Assets
- **Colors**: A specific palette (Rouge, Vert, Bleu, etc.) is defined and must be consistent across UI and backend logic.
- **Icons**: Shared SVG icons are stored in `shared/icons.js`.

### Versioning
- Versioning is driven by Git tags.
- `scripts/gen-version.js` generates version files during the build process.

### Testing Standard
- **Backend**: Go tests must run with `-race` to catch concurrency issues (enforced by `make test`).
- **Frontend**: Vitest is used for unit testing logic in `main.js`.
