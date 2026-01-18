import { test as base, Page } from '@playwright/test';

/**
 * WebSocket fixture for testing real-time communication
 * Uses page.evaluate to create WebSocket connections
 */
export class WebSocketFixture {
  private page: Page | null = null;
  private wsId: string | null = null;

  async connect(url: string = 'ws://localhost:8080/ws', page?: Page) {
    if (!page) {
      throw new Error('Page must be provided to connect WebSocket');
    }
    this.page = page;

    // Create a WebSocket connection using page.evaluate
    this.wsId = await page.evaluate(async (wsUrl) => {
      return new Promise<string>((resolve, reject) => {
        const ws = new WebSocket(wsUrl);
        const id = Math.random().toString(36).substring(7);

        (window as any).__testWebSockets = (window as any).__testWebSockets || {};
        (window as any).__testWebSockets[id] = ws;

        ws.onopen = () => {
          ws.send(JSON.stringify({ type: '_test_ready', id }));
        };

        ws.onmessage = (event) => {
          const data = JSON.parse(event.data);
          // Store messages
          if (!(window as any).__testWsMessages) {
            (window as any).__testWsMessages = {};
          }
          if (!(window as any).__testWsMessages[id]) {
            (window as any).__testWsMessages[id] = [];
          }
          (window as any).__testWsMessages[id].push(data);
        };

        ws.onerror = (error) => {
          reject(error);
        };

        // Wait for ready message
        const readyHandler = (event: MessageEvent) => {
          const data = JSON.parse(event.data);
          if (data.type === '_test_ready' && data.id === id) {
            ws.removeEventListener('message', readyHandler);
            resolve(id);
          }
        };
        ws.addEventListener('message', readyHandler);
      });
    }, url);

    return this;
  }

  async send(message: any) {
    if (!this.page || !this.wsId) throw new Error('WebSocket not connected');
    await this.page.evaluate(({ wsId, msg }) => {
      const ws = (window as any).__testWebSockets?.[wsId];
      if (ws) {
        ws.send(JSON.stringify(msg));
      }
    }, { wsId: this.wsId, msg });
  }

  async waitForMessage(type: string, timeout = 5000): Promise<any> {
    if (!this.page || !this.wsId) throw new Error('WebSocket not connected');

    return await this.page.waitForFunction(
      ({ wsId, messageType }: { wsId: string; messageType: string }) => {
        const messages = (window as any).__testWsMessages?.[wsId] || [];
        return messages.find((m: any) => m.type === messageType);
      },
      { wsId: this.wsId, messageType: type },
      { timeout }
    );
  }

  async getAllMessages(): Promise<any[]> {
    if (!this.page || !this.wsId) return [];
    return await this.page.evaluate(({ wsId }) => {
      return (window as any).__testWsMessages?.[wsId] || [];
    }, { wsId: this.wsId });
  }

  async clearMessages() {
    if (!this.page || !this.wsId) return;
    await this.page.evaluate(({ wsId }) => {
      if ((window as any).__testWsMessages) {
        (window as any).__testWsMessages[wsId] = [];
      }
    }, { wsId: this.wsId });
  }

  close() {
    if (this.page && this.wsId) {
      this.page.evaluate(({ wsId }) => {
        const ws = (window as any).__testWebSockets?.[wsId];
        if (ws) {
          ws.close();
        }
      }, { wsId: this.wsId }).catch(() => {});
    }
    this.page = null;
    this.wsId = null;
  }
}

/**
 * Extended test fixture with WebSocket support
 */
export const wsTest = base.extend<{
  ws: WebSocketFixture;
}>({
  ws: async ({ page }, use) => {
    const fixture = new WebSocketFixture();
    await use(fixture);
    fixture.close();
  },
});

/**
 * Helper to generate a random session code
 */
export function generateSessionCode(): string {
  return Math.floor(1000 + Math.random() * 9000).toString();
}

/**
 * Helper colors for testing
 */
export const TEST_COLORS = {
  red: '#ef4444',
  blue: '#3b82f6',
  green: '#22c55e',
  yellow: '#eab308',
  orange: '#f97316',
  violet: '#a855f7',
  rose: '#ec4899',
  gray: '#6b7280',
} as const;
