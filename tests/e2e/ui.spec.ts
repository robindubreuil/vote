import { test, expect, type BrowserContext, type Page } from '@playwright/test';
import { connectTrainer } from './ws-helper';

test.describe('UI E2E', () => {
  let trainerPage: Page;
  let stagiairePage: Page;

  test.beforeAll(async ({ browser }) => {
    const trainerCtx = await browser.newContext();
    const stagiaireCtx = await browser.newContext();
    trainerPage = await trainerCtx.newPage();
    stagiairePage = await stagiaireCtx.newPage();
  });

  test.afterAll(async () => {
    await trainerPage.context().close();
    await stagiairePage.context().close();
  });

  test('trainer creates session via UI', async () => {
    await trainerPage.goto('/formateur/');

    await trainerPage.getByTestId('create-session-btn').click();

    const codeBtn = trainerPage.getByTestId('session-code-btn');
    await expect(codeBtn).toBeVisible({ timeout: 10000 });
    const code = await codeBtn.textContent();
    expect(code).toMatch(/^\d{4}$/);

    await expect(trainerPage.getByTestId('start-vote-btn')).toBeVisible();
    await expect(trainerPage.getByTestId('start-vote-btn')).toBeDisabled();

    const checkboxes = trainerPage.locator('.color-checkbox input[type="checkbox"]:checked');
    expect(await checkboxes.count()).toBe(3);
  });

  test('stagiaire joins via UI', async () => {
    const codeBtn = trainerPage.getByTestId('session-code-btn');
    const code = await codeBtn.textContent();

    await stagiairePage.goto('/stagiaire/');
    await stagiairePage.getByTestId('name-input').fill('Marie');
    await stagiairePage.getByTestId('session-code-input').fill(code!);
    await stagiairePage.getByTestId('join-btn').click();

    await expect(stagiairePage.getByTestId('waiting-text')).toBeVisible({ timeout: 10000 });
    await expect(stagiairePage.getByTestId('waiting-name')).toContainText('Marie');

    const countEl = trainerPage.getByTestId('connected-count');
    await expect(countEl).toContainText('1 stagiaire');
  });

  test('full vote flow', async () => {
    await trainerPage.getByTestId('start-vote-btn').click();

    await expect(stagiairePage.getByTestId('vote-instruction')).toBeVisible({ timeout: 10000 });

    const rougeBtn = stagiairePage.getByTestId('vote-btn-rouge');
    if (await rougeBtn.isVisible()) {
      await rougeBtn.click();
    }

    await expect(stagiairePage.getByTestId('voted-title')).toBeVisible({ timeout: 10000 });

    const countEl = trainerPage.getByTestId('vote-count');
    await expect(countEl).toContainText('1 / 1', { timeout: 10000 });

    await trainerPage.getByTestId('close-vote-btn').click();

    await expect(stagiairePage.getByTestId('vote-closed-text')).toBeVisible({ timeout: 10000 });

    await expect(trainerPage.getByTestId('new-vote-btn')).toBeVisible();
  });

  test('multiple choice toggle persists across votes', async () => {
    await trainerPage.getByTestId('new-vote-btn').click();

    await expect(trainerPage.getByTestId('multiple-choice-toggle')).toBeVisible({ timeout: 10000 });

    await trainerPage.getByTestId('multiple-choice-toggle').click();

    const toggle = trainerPage.locator('.toggle-switch.active');
    await expect(toggle).toBeVisible();

    await trainerPage.getByTestId('start-vote-btn').click();

    await expect(stagiairePage.getByTestId('vote-instruction')).toBeVisible({ timeout: 10000 });
    const instruction = stagiairePage.getByTestId('vote-instruction');
    await expect(instruction).toHaveClass(/multiple-choice/);

    const checkbox = stagiairePage.getByTestId('vote-checkbox-rouge');
    if (await checkbox.isVisible()) {
      await checkbox.check();
    }
    await stagiairePage.getByTestId('submit-vote-btn').click();
    await expect(stagiairePage.getByTestId('voted-title')).toBeVisible({ timeout: 10000 });

    await trainerPage.getByTestId('close-vote-btn').click();
    await expect(trainerPage.getByTestId('new-vote-btn')).toBeVisible({ timeout: 10000 });
  });

  test('stagiaire name update', async () => {
    await trainerPage.getByTestId('new-vote-btn').click();
    await expect(trainerPage.getByTestId('start-vote-btn')).toBeVisible({ timeout: 10000 });

    await stagiairePage.getByTestId('edit-name-btn').click();

    await expect(stagiairePage.getByTestId('edit-name-form')).toBeVisible();

    const nameInput = stagiairePage.getByTestId('edit-name-input');
    await nameInput.clear();
    await nameInput.fill('Marie Dupont');
    await stagiairePage.locator('#editNameForm button[type="submit"]').click();

    await expect(stagiairePage.getByTestId('waiting-name')).toContainText('Marie Dupont', { timeout: 10000 });
  });

  test('vote reset to config', async () => {
    await trainerPage.getByTestId('start-vote-btn').click();
    await expect(stagiairePage.getByTestId('vote-instruction')).toBeVisible({ timeout: 10000 });

    await trainerPage.getByTestId('close-vote-btn').click();
    await expect(trainerPage.getByTestId('new-vote-btn')).toBeVisible({ timeout: 10000 });

    await trainerPage.getByTestId('new-vote-btn').click();

    await expect(trainerPage.getByTestId('start-vote-btn')).toBeVisible({ timeout: 10000 });
    const checkboxes = trainerPage.locator('.color-checkbox input[type="checkbox"]:checked');
    expect(await checkboxes.count()).toBeGreaterThanOrEqual(2);
  });

  test('trainer leave session', async () => {
    await trainerPage.getByTestId('session-code-btn').click();

    trainerPage.once('dialog', dialog => dialog.accept());
    await trainerPage.getByTestId('session-code-btn').click();

    await expect(trainerPage.getByTestId('create-session-btn')).toBeVisible({ timeout: 10000 });
  });
});
