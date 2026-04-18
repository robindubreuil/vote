import WebSocket from 'ws';

const WS_URL = process.env.WS_URL || 'ws://localhost:8080/ws';

interface WSClient {
  ws: WebSocket;
  messages: any[];
  waitForMessage: (type: string, timeout?: number) => Promise<any>;
  waitForMessages: (type: string, count: number, timeout?: number) => Promise<any[]>;
  send: (data: any) => void;
  dispose: () => void;
}

function createClient(url: string): Promise<WSClient> {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(url);
    const pending: any[] = [];
    const waiterQueue: { type: string; resolve: (msg: any) => void }[] = [];

    ws.on('message', (data: WebSocket.Data) => {
      const msg = JSON.parse(data.toString());

      const waiterIdx = waiterQueue.findIndex(w => w.type === msg.type);
      if (waiterIdx !== -1) {
        const waiter = waiterQueue.splice(waiterIdx, 1)[0];
        waiter.resolve(msg);
      } else {
        pending.push(msg);
      }
    });

    ws.on('open', () => {
      const client: WSClient = {
        ws,
        get messages() {
          return pending;
        },
        waitForMessage: (type: string, timeout = 5000): Promise<any> => {
          return new Promise((resolve, reject) => {
            const idx = pending.findIndex(m => m.type === type);
            if (idx !== -1) {
              return resolve(pending.splice(idx, 1)[0]);
            }

            const timer = setTimeout(() => {
              const wi = waiterQueue.findIndex(w => w.resolve === wrappedResolve);
              if (wi !== -1) waiterQueue.splice(wi, 1);
              reject(new Error(`Timeout waiting for "${type}". Pending: [${pending.map(m => m.type).join(', ')}]`));
            }, timeout);

            const wrappedResolve = (msg: any) => {
              clearTimeout(timer);
              resolve(msg);
            };

            waiterQueue.push({ type, resolve: wrappedResolve });
          });
        },
        waitForMessages: (type: string, count: number, timeout = 5000): Promise<any[]> => {
          const results: any[] = [];
          const promises: Promise<any>[] = [];
          for (let i = 0; i < count; i++) {
            promises.push(client.waitForMessage(type, timeout));
          }
          return Promise.all(promises);
        },
        send: (data: any) => {
          ws.send(JSON.stringify(data));
        },
        dispose: () => {
          waiterQueue.length = 0;
          if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
            ws.close();
          }
        },
      };
      resolve(client);
    });

    ws.on('error', (err) => reject(err));
  });
}

export async function connectTrainer(
  sessionCode: string | null,
  trainerId?: string
): Promise<WSClient> {
  const client = await createClient(WS_URL);
  client.send({
    type: 'trainer_join',
    sessionCode: sessionCode ?? '',
    ...(trainerId ? { trainerId } : {}),
  });
  return client;
}

export async function connectStagiaire(
  sessionCode: string,
  stagiaireId?: string,
  name?: string
): Promise<WSClient> {
  const client = await createClient(WS_URL);
  client.send({
    type: 'stagiaire_join',
    sessionCode,
    ...(stagiaireId ? { stagiaireId } : {}),
    ...(name ? { name } : {}),
  });
  return client;
}
