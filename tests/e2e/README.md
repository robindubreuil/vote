# Vote Coloré - E2E Tests

End-to-end tests for the Vote Coloré application using Playwright.

## Current Status

The E2E tests are **basic** and focus on backend verification:
- Health endpoint correctness
- WebSocket endpoint connectivity
- Helper function validation
- CORS headers

## Setup

```bash
cd tests/e2e
npm install
npx playwright install chromium
```

## Running Tests

**Option 1: With auto-started backend**
```bash
npm test
```

**Option 2: With manually started backend**
```bash
# Start the backend first
cd ../backend && ./vote-server

# Then run tests (skip webServer)
cd tests/e2e
SKIP_WS=1 npm test
```

## Test Coverage

### Passing Tests (4)
| Test | Description |
|------|-------------|
| Health Check › health endpoint responds | Verifies `/health` returns 200 with `{status: "ok"}` |
| WebSocket Endpoint › websocket endpoint exists | Verifies WebSocket endpoint at `/ws` accepts connections |
| Helper Functions › generateSessionCode creates valid 4-digit codes | Validates session code generation (1000-9999) |
| API Response Headers › CORS headers are present | Checks response headers are present |

## Architecture Notes

**Important:** The backend does **not** serve the frontend files. The frontends are separate applications that must be run independently:

- **Backend**: Go server (port 8080) - handles WebSocket and health endpoint
- **Formateur Frontend**: Vite dev server (default port 5173) - `cd frontend-formateur && npm run dev`
- **Stagiaire Frontend**: Vite dev server (default port 5174) - `cd frontend-stagiaire && npm run dev`

## Running Full Stack Tests

To test the complete application:

```bash
# Terminal 1: Backend
cd backend && ./vote-server

# Terminal 2: Formateur
cd frontend-formateur && npm run dev

# Terminal 3: Stagiaire
cd frontend-stagiaire && npm run dev

# Then run tests against each frontend
cd tests/e2e
# Update baseURL to point to the specific frontend
BASE_URL=http://localhost:5173 SKIP_WS=1 npm test
```

## Future Enhancements

To add comprehensive E2E test coverage:

1. **Add test-specific attributes** to frontend elements for reliable selection:
   ```html
   <button data-testid="start-vote-button">Start Vote</button>
   <input data-testid="session-code-input" />
   ```

2. **Test complete voting scenarios:**
   - Trainer creates session, stagiaires join, vote casting, results display
   - Multiple choice voting scenarios
   - Vote close/reset functionality
   - Reconnection handling

3. **Test WebSocket message flows:**
   - Session creation flow
   - Vote submission flow
   - Stagiaire name updates

4. **Add WebSocket integration tests** using the WebSocket fixture in `fixtures.ts`

5. **Test error handling:**
   - Invalid session codes
   - Connection failures
   - Malformed messages

## WebSocket Fixture Usage

The `WebSocketFixture` class in `fixtures.ts` provides helper methods for WebSocket communication:

```typescript
import { WebSocketFixture } from './fixtures';

test('websocket communication', async ({ page }) => {
  const ws = new WebSocketFixture();
  await ws.connect('ws://localhost:8080/ws', page);

  // Send message
  await ws.send({ type: 'trainer_join', sessionCode: '1234', trainerId: 'test' });

  // Wait for specific message type
  const msg = await ws.waitForMessage('session_created');
  expect(msg.sessionCode).toBe('1234');

  ws.close();
});
```

## Environment Variables

- `BASE_URL` - Override base URL (default: `http://localhost:8080`)
- `SKIP_WS` - Skip backend auto-start (use if backend is already running)

## Dependencies

- `@playwright/test` - Browser automation framework
- `chromium` - Browser for running tests
