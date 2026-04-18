import { test, expect } from '@playwright/test';
import { connectTrainer, connectStagiaire } from './ws-helper';

test.describe('HTTP Endpoints', () => {

  test('health endpoint responds', async ({ request }) => {
    const response = await request.get('http://localhost:8080/health');
    expect(response.status()).toBe(200);
    const body = await response.json();
    expect(body.status).toBe('ok');
    expect(typeof body.uptime_seconds).toBe('number');
    expect(body.metrics).toBeDefined();
  });

  test('metrics endpoint returns Prometheus format', async ({ request }) => {
    const response = await request.get('http://localhost:8080/metrics');
    expect(response.status()).toBe(200);
    const body = await response.text();
    expect(body).toContain('# HELP vote_uptime_seconds');
    expect(body).toContain('# TYPE vote_uptime_seconds gauge');
    expect(body).toContain('vote_sessions_active');
    expect(body).toContain('vote_trainers_connected');
    expect(body).toContain('vote_stagiaires_connected');
    expect(body).toContain('vote_sessions_by_state');
    expect(body).toContain('go_goroutines');
    expect(body).toContain('go_mem_alloc_bytes');
    expect(body).toContain('go_gc_total');
    expect(body).toContain('vote_build_info');
  });

  test('metrics reflect active sessions', async ({ request }) => {
    const trainer = await connectTrainer(null);
    await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const response = await request.get('http://localhost:8080/metrics');
    const body = await response.text();
    expect(body).toMatch(/vote_sessions_active [1-9]/);
    expect(body).toMatch(/vote_trainers_connected [1-9]/);

    trainer.dispose();
  });
});

test.describe('WS Protocol', () => {

  test('trainer creates session', async () => {
    const trainer = await connectTrainer(null);
    const created = await trainer.waitForMessage('session_created');
    expect(created.sessionCode).toMatch(/^\d{4}$/);
    expect(created.trainerId).toBeTruthy();

    const count = await trainer.waitForMessage('connected_count');
    expect(count.count).toBe(0);
    expect(count.stagiaires).toEqual([]);

    trainer.dispose();
  });

  test('trainer rejoins existing session', async () => {
    const trainer1 = await connectTrainer(null);
    const created = await trainer1.waitForMessage('session_created');
    const code = created.sessionCode;
    await trainer1.waitForMessage('connected_count');
    trainer1.dispose();

    const trainer2 = await connectTrainer(code);
    const created2 = await trainer2.waitForMessage('session_created');
    expect(created2.sessionCode).toBe(code);

    const count = await trainer2.waitForMessage('connected_count');
    expect(count.count).toBe(0);

    trainer2.dispose();
  });

  test('stagiaire joins session', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const stagiaire = await connectStagiaire(sessionCode, undefined, 'Alice');
    const joined = await stagiaire.waitForMessage('session_joined');
    expect(joined.sessionCode).toBe(sessionCode);
    expect(joined.stagiaireId).toBeTruthy();

    const count = await trainer.waitForMessage('connected_count');
    expect(count.count).toBe(1);
    expect(count.stagiaires).toHaveLength(1);
    expect(count.stagiaires[0].name).toBe('Alice');
    expect(count.stagiaires[0].connected).toBe(true);

    trainer.dispose();
    stagiaire.dispose();
  });

  test('multiple stagiaires join same session', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const names = ['Alice', 'Bob', 'Charlie'];
    const stagiaires = [];

    let lastCount: any;
    for (const name of names) {
      const s = await connectStagiaire(sessionCode, undefined, name);
      await s.waitForMessage('session_joined');
      lastCount = await trainer.waitForMessage('connected_count');
      expect(lastCount.count).toBe(stagiaires.length + 1);
      stagiaires.push(s);
    }

    expect(lastCount.count).toBe(3);
    const joinedNames = lastCount.stagiaires.map((s: any) => s.name);
    expect(joinedNames).toContain('Alice');
    expect(joinedNames).toContain('Bob');
    expect(joinedNames).toContain('Charlie');

    trainer.dispose();
    stagiaires.forEach(s => s.dispose());
  });

  test('stagiaire rejected on bad code', async () => {
    const stagiaire = await connectStagiaire('9999', undefined, 'Test');
    const error = await stagiaire.waitForMessage('error');
    expect(error.message).toContain('Session introuvable');
    stagiaire.dispose();
  });

  test('stagiaire rejected when no trainer connected', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');
    trainer.dispose();

    await new Promise(r => setTimeout(r, 200));

    const stagiaire = await connectStagiaire(sessionCode, undefined, 'Alice');
    const error = await stagiaire.waitForMessage('error');
    expect(error.message).toContain('no trainer');
    stagiaire.dispose();
  });

  test('full vote cycle with single stagiaire', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const stagiaire = await connectStagiaire(sessionCode, undefined, 'Alice');
    await stagiaire.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    trainer.send({ type: 'start_vote', colors: ['rouge', 'vert', 'bleu'], multipleChoice: false });
    const voteStarted = await stagiaire.waitForMessage('vote_started');
    expect(voteStarted.colors).toEqual(['rouge', 'vert', 'bleu']);
    expect(voteStarted.multipleChoice).toBe(false);

    await trainer.waitForMessage('connected_count');

    stagiaire.send({ type: 'vote', colors: ['rouge'], stagiaireId: 'ignored' });
    await stagiaire.waitForMessage('vote_accepted');

    const voteReceived = await trainer.waitForMessage('vote_received');
    expect(voteReceived.colors).toEqual(['rouge']);
    expect(voteReceived.stagiaireName).toBe('Alice');

    trainer.send({ type: 'close_vote' });
    await stagiaire.waitForMessage('vote_closed');

    trainer.dispose();
    stagiaire.dispose();
  });

  test('full vote cycle with multiple stagiaires', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const s1 = await connectStagiaire(sessionCode, undefined, 'Alice');
    await s1.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    const s2 = await connectStagiaire(sessionCode, undefined, 'Bob');
    await s2.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    const s3 = await connectStagiaire(sessionCode, undefined, 'Charlie');
    await s3.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    trainer.send({ type: 'start_vote', colors: ['rouge', 'vert', 'bleu'], multipleChoice: false });

    await s1.waitForMessage('vote_started');
    await s2.waitForMessage('vote_started');
    await s3.waitForMessage('vote_started');

    s1.send({ type: 'vote', colors: ['rouge'] });
    await s1.waitForMessage('vote_accepted');
    const vr1 = await trainer.waitForMessage('vote_received');
    expect(vr1.stagiaireName).toBe('Alice');
    expect(vr1.colors).toEqual(['rouge']);

    s2.send({ type: 'vote', colors: ['vert'] });
    await s2.waitForMessage('vote_accepted');
    const vr2 = await trainer.waitForMessage('vote_received');
    expect(vr2.stagiaireName).toBe('Bob');
    expect(vr2.colors).toEqual(['vert']);

    s3.send({ type: 'vote', colors: ['bleu'] });
    await s3.waitForMessage('vote_accepted');
    const vr3 = await trainer.waitForMessage('vote_received');
    expect(vr3.stagiaireName).toBe('Charlie');
    expect(vr3.colors).toEqual(['bleu']);

    const countAfterVotes = trainer.messages
      .filter(m => m.type === 'connected_count')
      .pop();
    expect(countAfterVotes.count).toBe(3);
    const votedStagiaires = countAfterVotes.stagiaires.filter(s => s.vote && s.vote.length > 0);
    expect(votedStagiaires).toHaveLength(3);

    trainer.send({ type: 'close_vote' });
    await s1.waitForMessage('vote_closed');
    await s2.waitForMessage('vote_closed');
    await s3.waitForMessage('vote_closed');

    trainer.dispose();
    s1.dispose();
    s2.dispose();
    s3.dispose();
  });

  test('multiple choice vote with multiple stagiaires', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const s1 = await connectStagiaire(sessionCode, undefined, 'Alice');
    await s1.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    const s2 = await connectStagiaire(sessionCode, undefined, 'Bob');
    await s2.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    trainer.send({ type: 'start_vote', colors: ['rouge', 'vert'], multipleChoice: true });

    const vs1 = await s1.waitForMessage('vote_started');
    expect(vs1.multipleChoice).toBe(true);
    const vs2 = await s2.waitForMessage('vote_started');
    expect(vs2.multipleChoice).toBe(true);

    s1.send({ type: 'vote', colors: ['rouge', 'vert'] });
    await s1.waitForMessage('vote_accepted');
    const vr1 = await trainer.waitForMessage('vote_received');
    expect(vr1.colors.sort()).toEqual(['rouge', 'vert']);

    s2.send({ type: 'vote', colors: ['rouge'] });
    await s2.waitForMessage('vote_accepted');
    const vr2 = await trainer.waitForMessage('vote_received');
    expect(vr2.colors).toEqual(['rouge']);

    trainer.dispose();
    s1.dispose();
    s2.dispose();
  });

  test('single choice enforced', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const stagiaire = await connectStagiaire(sessionCode, undefined, 'Alice');
    await stagiaire.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    trainer.send({ type: 'start_vote', colors: ['rouge', 'vert'], multipleChoice: false });
    await stagiaire.waitForMessage('vote_started');

    stagiaire.send({ type: 'vote', colors: ['rouge', 'vert'] });
    const error = await stagiaire.waitForMessage('error');
    expect(error.message).toContain('one color');

    trainer.dispose();
    stagiaire.dispose();
  });

  test('vote rejected when not active', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const stagiaire = await connectStagiaire(sessionCode, undefined, 'Alice');
    await stagiaire.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    stagiaire.send({ type: 'vote', colors: ['rouge'] });
    const error = await stagiaire.waitForMessage('error');
    expect(error.message).toContain('not active');

    trainer.dispose();
    stagiaire.dispose();
  });

  test('vote rejected with invalid color', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const stagiaire = await connectStagiaire(sessionCode, undefined, 'Alice');
    await stagiaire.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    trainer.send({ type: 'start_vote', colors: ['rouge', 'vert'], multipleChoice: false });
    await stagiaire.waitForMessage('vote_started');

    stagiaire.send({ type: 'vote', colors: ['bleu'] });
    const error = await stagiaire.waitForMessage('error');
    expect(error.message).toContain('invalid color');

    trainer.dispose();
    stagiaire.dispose();
  });

  test('vote rejected with empty colors', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const stagiaire = await connectStagiaire(sessionCode, undefined, 'Alice');
    await stagiaire.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    trainer.send({ type: 'start_vote', colors: ['rouge', 'vert'], multipleChoice: true });
    await stagiaire.waitForMessage('vote_started');

    stagiaire.send({ type: 'vote', colors: [] });
    const error = await stagiaire.waitForMessage('error');
    expect(error.message).toContain('at least one color');

    trainer.dispose();
    stagiaire.dispose();
  });

  test('vote reset preserves config for next start', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const stagiaire = await connectStagiaire(sessionCode, undefined, 'Alice');
    await stagiaire.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    trainer.send({ type: 'start_vote', colors: ['rouge', 'vert'], multipleChoice: true });
    await stagiaire.waitForMessage('vote_started');
    await trainer.waitForMessage('connected_count');

    stagiaire.send({ type: 'vote', colors: ['rouge'] });
    await stagiaire.waitForMessage('vote_accepted');
    await trainer.waitForMessage('vote_received');

    trainer.send({ type: 'close_vote' });
    await stagiaire.waitForMessage('vote_closed');

    trainer.send({ type: 'reset_vote', colors: ['bleu', 'jaune'], multipleChoice: false });
    await stagiaire.waitForMessage('vote_reset');
    await trainer.waitForMessage('connected_count');

    trainer.send({ type: 'start_vote', colors: ['bleu', 'jaune'], multipleChoice: false });
    const vs = await stagiaire.waitForMessage('vote_started');
    expect(vs.colors).toEqual(['bleu', 'jaune']);
    expect(vs.multipleChoice).toBe(false);

    trainer.dispose();
    stagiaire.dispose();
  });

  test('trainer reconnect restores active vote state', async () => {
    const trainer1 = await connectTrainer(null);
    const { sessionCode, trainerId } = await trainer1.waitForMessage('session_created');
    await trainer1.waitForMessage('connected_count');

    const s1 = await connectStagiaire(sessionCode, undefined, 'Alice');
    await s1.waitForMessage('session_joined');
    await trainer1.waitForMessage('connected_count');

    trainer1.send({ type: 'start_vote', colors: ['rouge', 'vert'], multipleChoice: false });
    await s1.waitForMessage('vote_started');
    await trainer1.waitForMessage('connected_count');

    s1.send({ type: 'vote', colors: ['rouge'] });
    await s1.waitForMessage('vote_accepted');
    await trainer1.waitForMessage('vote_received');

    trainer1.dispose();

    const trainer2 = await connectTrainer(sessionCode);
    const created = await trainer2.waitForMessage('session_created');
    expect(created.sessionCode).toBe(sessionCode);

    const restored = await trainer2.waitForMessage('vote_started');
    expect(restored.colors).toEqual(['rouge', 'vert']);
    expect(restored.multipleChoice).toBe(false);

    const voteReplay = await trainer2.waitForMessage('vote_received');
    expect(voteReplay.colors).toEqual(['rouge']);
    expect(voteReplay.stagiaireName).toBe('Alice');

    const count = await trainer2.waitForMessage('connected_count');
    expect(count.count).toBe(1);

    trainer2.dispose();
    s1.dispose();
  });

  test('trainer reconnect restores idle state with config', async () => {
    const trainer1 = await connectTrainer(null);
    const { sessionCode } = await trainer1.waitForMessage('session_created');
    await trainer1.waitForMessage('connected_count');

    trainer1.send({ type: 'start_vote', colors: ['rouge', 'vert'], multipleChoice: true });
    await trainer1.waitForMessage('connected_count');

    trainer1.send({ type: 'reset_vote', colors: ['rouge', 'vert'], multipleChoice: true });
    await trainer1.waitForMessage('connected_count');

    trainer1.dispose();

    const trainer2 = await connectTrainer(sessionCode);
    await trainer2.waitForMessage('session_created');

    const config = await trainer2.waitForMessage('config_updated');
    expect(config.selectedColors).toEqual(['rouge', 'vert']);
    expect(config.multipleChoice).toBe(true);

    await trainer2.waitForMessage('connected_count');

    trainer2.dispose();
  });

  test('stagiaire name update', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const stagiaire = await connectStagiaire(sessionCode, undefined, 'Alice');
    const joined = await stagiaire.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    stagiaire.send({ type: 'update_name', stagiaireId: joined.stagiaireId, name: 'Alicia' });
    await stagiaire.waitForMessage('name_updated');

    const namesUpdated = await trainer.waitForMessage('stagiaire_names_updated');
    expect(namesUpdated.stagiaires).toHaveLength(1);
    expect(namesUpdated.stagiaires[0].name).toBe('Alicia');

    trainer.dispose();
    stagiaire.dispose();
  });

  test('duplicate trainer kicks old connection', async () => {
    const trainer1 = await connectTrainer(null);
    const { sessionCode } = await trainer1.waitForMessage('session_created');
    await trainer1.waitForMessage('connected_count');

    const trainer2 = await connectTrainer(sessionCode);
    await trainer2.waitForMessage('session_created');

    const error = await trainer1.waitForMessage('error');
    expect(error.message).toContain('New trainer');

    await trainer2.waitForMessage('connected_count');

    trainer1.dispose();
    trainer2.dispose();
  });

  test('stagiaire reconnect by ID restores session', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const s1 = await connectStagiaire(sessionCode, undefined, 'Alice');
    const joined = await s1.waitForMessage('session_joined');
    const stagiaireId = joined.stagiaireId;
    await trainer.waitForMessage('connected_count');

    trainer.send({ type: 'start_vote', colors: ['rouge', 'vert'], multipleChoice: false });
    await s1.waitForMessage('vote_started');
    await trainer.waitForMessage('connected_count');

    s1.dispose();

    const s2 = await connectStagiaire(sessionCode, stagiaireId, 'Alice');
    const joined2 = await s2.waitForMessage('session_joined');
    expect(joined2.stagiaireId).toBe(stagiaireId);

    const voteStarted = await s2.waitForMessage('vote_started');
    expect(voteStarted.colors).toEqual(['rouge', 'vert']);

    trainer.dispose();
    s2.dispose();
  });

  test('multiple sessions are isolated', async () => {
    const trainerA = await connectTrainer(null);
    const { sessionCode: codeA } = await trainerA.waitForMessage('session_created');
    await trainerA.waitForMessage('connected_count');

    const trainerB = await connectTrainer(null);
    const { sessionCode: codeB } = await trainerB.waitForMessage('session_created');
    await trainerB.waitForMessage('connected_count');

    expect(codeA).not.toBe(codeB);

    const sA = await connectStagiaire(codeA, undefined, 'Alice');
    await sA.waitForMessage('session_joined');
    await trainerA.waitForMessage('connected_count');

    const sB = await connectStagiaire(codeB, undefined, 'Bob');
    await sB.waitForMessage('session_joined');
    await trainerB.waitForMessage('connected_count');

    trainerA.send({ type: 'start_vote', colors: ['rouge'], multipleChoice: false });
    await sA.waitForMessage('vote_started');

    const sBMessages = sB.messages.filter(m => m.type === 'vote_started');
    expect(sBMessages).toHaveLength(0);

    trainerB.send({ type: 'start_vote', colors: ['bleu'], multipleChoice: false });
    await sB.waitForMessage('vote_started');

    sA.send({ type: 'vote', colors: ['rouge'] });
    await sA.waitForMessage('vote_accepted');
    const vrA = await trainerA.waitForMessage('vote_received');
    expect(vrA.colors).toEqual(['rouge']);

    sB.send({ type: 'vote', colors: ['bleu'] });
    await sB.waitForMessage('vote_accepted');
    const vrB = await trainerB.waitForMessage('vote_received');
    expect(vrB.colors).toEqual(['bleu']);

    trainerA.dispose();
    trainerB.dispose();
    sA.dispose();
    sB.dispose();
  });

  test('stagiaire disconnect decrements count', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const s1 = await connectStagiaire(sessionCode, undefined, 'Alice');
    await s1.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    const s2 = await connectStagiaire(sessionCode, undefined, 'Bob');
    await s2.waitForMessage('session_joined');
    const count2 = await trainer.waitForMessage('connected_count');
    expect(count2.count).toBe(2);

    s1.dispose();

    const countAfter = await trainer.waitForMessage('connected_count', 3000);
    expect(countAfter.count).toBe(1);
    const connected = countAfter.stagiaires.filter((s: any) => s.connected);
    expect(connected).toHaveLength(1);
    expect(connected[0].name).toBe('Bob');

    trainer.dispose();
    s2.dispose();
  });

  test('name collision rejected', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const s1 = await connectStagiaire(sessionCode, undefined, 'Alice');
    await s1.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    const s2 = await connectStagiaire(sessionCode, undefined, 'Alice');
    const error = await s2.waitForMessage('error');
    expect(error.message.toLowerCase()).toContain('nom');

    trainer.dispose();
    s1.dispose();
    s2.dispose();
  });

  test('start vote with labels', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const stagiaire = await connectStagiaire(sessionCode, undefined, 'Alice');
    await stagiaire.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    trainer.send({
      type: 'start_vote',
      colors: ['rouge', 'vert'],
      multipleChoice: false,
      labels: { rouge: 'Oui', vert: 'Non' },
    });

    const vs = await stagiaire.waitForMessage('vote_started');
    expect(vs.labels).toEqual({ rouge: 'Oui', vert: 'Non' });

    trainer.dispose();
    stagiaire.dispose();
  });

  test('close vote then reset starts fresh cycle', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const s1 = await connectStagiaire(sessionCode, undefined, 'Alice');
    await s1.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    const s2 = await connectStagiaire(sessionCode, undefined, 'Bob');
    await s2.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    trainer.send({ type: 'start_vote', colors: ['rouge', 'vert'], multipleChoice: false });
    await s1.waitForMessage('vote_started');
    await s2.waitForMessage('vote_started');
    await trainer.waitForMessage('connected_count');

    s1.send({ type: 'vote', colors: ['rouge'] });
    await s1.waitForMessage('vote_accepted');
    await trainer.waitForMessage('vote_received');

    s2.send({ type: 'vote', colors: ['vert'] });
    await s2.waitForMessage('vote_accepted');
    await trainer.waitForMessage('vote_received');

    trainer.send({ type: 'close_vote' });
    await s1.waitForMessage('vote_closed');
    await s2.waitForMessage('vote_closed');

    trainer.send({ type: 'reset_vote', colors: ['bleu', 'jaune'], multipleChoice: true });
    await s1.waitForMessage('vote_reset');
    await s2.waitForMessage('vote_reset');
    await trainer.waitForMessage('connected_count');

    trainer.send({ type: 'start_vote', colors: ['bleu', 'jaune'], multipleChoice: true });
    const vs1 = await s1.waitForMessage('vote_started');
    expect(vs1.colors).toEqual(['bleu', 'jaune']);
    expect(vs1.multipleChoice).toBe(true);

    const vs2 = await s2.waitForMessage('vote_started');
    expect(vs2.colors).toEqual(['bleu', 'jaune']);

    s1.send({ type: 'vote', colors: ['bleu', 'jaune'] });
    await s1.waitForMessage('vote_accepted');

    const vr = await trainer.waitForMessage('vote_received');
    expect(vr.colors.sort()).toEqual(['bleu', 'jaune']);

    trainer.dispose();
    s1.dispose();
    s2.dispose();
  });

  test('start vote validation - empty colors', async () => {
    const trainer = await connectTrainer(null);
    await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    trainer.send({ type: 'start_vote', colors: [], multipleChoice: false });
    const error = await trainer.waitForMessage('error');
    expect(error.message).toContain('color');

    trainer.dispose();
  });

  test('start vote validation - invalid color', async () => {
    const trainer = await connectTrainer(null);
    await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    trainer.send({ type: 'start_vote', colors: ['magenta'], multipleChoice: false });
    const error = await trainer.waitForMessage('error');
    expect(error.message).toContain('Invalid color');

    trainer.dispose();
  });

  test('start vote validation - duplicate colors', async () => {
    const trainer = await connectTrainer(null);
    await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    trainer.send({ type: 'start_vote', colors: ['rouge', 'rouge'], multipleChoice: false });
    const error = await trainer.waitForMessage('error');
    expect(error.message).toContain('Duplicate');

    trainer.dispose();
  });

  test('non-trainer cannot start vote', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const stagiaire = await connectStagiaire(sessionCode, undefined, 'Alice');
    await stagiaire.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    stagiaire.send({ type: 'start_vote', colors: ['rouge', 'vert'], multipleChoice: false });
    const error = await stagiaire.waitForMessage('error');
    expect(error.message).toContain('unauthorized');

    trainer.dispose();
    stagiaire.dispose();
  });

  test('non-trainer cannot close vote', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const stagiaire = await connectStagiaire(sessionCode, undefined, 'Alice');
    await stagiaire.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    trainer.send({ type: 'start_vote', colors: ['rouge'], multipleChoice: false });
    await stagiaire.waitForMessage('vote_started');

    stagiaire.send({ type: 'close_vote' });
    const error = await stagiaire.waitForMessage('error');
    expect(error.message).toContain('unauthorized');

    trainer.dispose();
    stagiaire.dispose();
  });

  test('update name validation - too long', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const stagiaire = await connectStagiaire(sessionCode, undefined, 'Alice');
    await stagiaire.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    stagiaire.send({ type: 'update_name', name: 'A'.repeat(20) });
    const error = await stagiaire.waitForMessage('error');
    expect(error.message).toContain('invalid');

    trainer.dispose();
    stagiaire.dispose();
  });

  test('concurrent votes from multiple stagiaires all counted', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const stagiaires = [];
    for (let i = 0; i < 5; i++) {
      const s = await connectStagiaire(sessionCode, undefined, `Stagiaire${i}`);
      await s.waitForMessage('session_joined');
      await trainer.waitForMessage('connected_count');
      stagiaires.push(s);
    }

    trainer.send({ type: 'start_vote', colors: ['rouge', 'vert'], multipleChoice: false });
    for (const s of stagiaires) {
      await s.waitForMessage('vote_started');
    }
    await trainer.waitForMessage('connected_count');

    const colors = ['rouge', 'vert'];
    const votePromises = stagiaires.map((s, i) => {
      s.send({ type: 'vote', colors: [colors[i % 2]] });
      return s.waitForMessage('vote_accepted');
    });
    await Promise.all(votePromises);

    const receivedNames: string[] = [];
    for (let i = 0; i < 5; i++) {
      const vr = await trainer.waitForMessage('vote_received');
      receivedNames.push(vr.stagiaireName);
    }

    expect(receivedNames.sort()).toEqual(
      ['Stagiaire0', 'Stagiaire1', 'Stagiaire2', 'Stagiaire3', 'Stagiaire4'].sort()
    );

    trainer.dispose();
    stagiaires.forEach(s => s.dispose());
  });

  test('stagiaire sees existing vote on reconnect during active vote', async () => {
    const trainer = await connectTrainer(null);
    const { sessionCode } = await trainer.waitForMessage('session_created');
    await trainer.waitForMessage('connected_count');

    const s1 = await connectStagiaire(sessionCode, undefined, 'Alice');
    const { stagiaireId } = await s1.waitForMessage('session_joined');
    await trainer.waitForMessage('connected_count');

    trainer.send({ type: 'start_vote', colors: ['rouge', 'vert'], multipleChoice: false });
    await s1.waitForMessage('vote_started');
    await trainer.waitForMessage('connected_count');

    s1.send({ type: 'vote', colors: ['rouge'] });
    await s1.waitForMessage('vote_accepted');
    await trainer.waitForMessage('vote_received');

    s1.dispose();

    const s2 = await connectStagiaire(sessionCode, stagiaireId, 'Alice');
    await s2.waitForMessage('session_joined');

    const voteStarted = await s2.waitForMessage('vote_started');
    expect(voteStarted.existingVote).toEqual(['rouge']);

    trainer.dispose();
    s2.dispose();
  });
});
