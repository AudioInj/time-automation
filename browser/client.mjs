import { chromium } from 'playwright';
import { readFile, mkdir, writeFile, rename, rm } from 'node:fs/promises';
import { dirname } from 'node:path';
import { pathToFileURL } from 'node:url';

function origin(value) {
  const u = new URL(value);
  if (u.protocol !== 'https:' || u.origin !== value || u.username || u.password) throw Error('Invalid origin');
  return u.origin;
}
async function session(context, cfg) {
  const r = await context.request.get(`${cfg.origin}/api/session`, { maxRedirects: 0, timeout: 15000 });
  if (r.status() === 401) return null;
  if (r.status() !== 200 || !r.headers()['content-type']?.includes('application/json')) throw Error('Invalid session response');
  const s = await r.json();
  if (!s.csrfToken || s.user?.username !== cfg.username.trim().toLowerCase()) throw Error('Session identity mismatch');
  return s;
}

export async function login(context, cfg) {
  const page = await context.newPage();
  try {
    await page.goto(`${cfg.origin}/login`, { waitUntil: 'domcontentloaded' });
    const deadline = Date.now() + 60000;
    let usernameSent = false, passwordSent = false;
    while (Date.now() < deadline) {
      const current = new URL(page.url()).origin;
      if (current === cfg.origin) {
        const s = await session(context, cfg);
        if (s) return s;
      } else if (current === cfg.authOrigin) {
        // Playwright locators pierce Authentik's open shadow roots.
        const username = page.locator('input[name="uid"], input[name="username"], input[autocomplete="username"]').first();
        const password = page.locator('input[type="password"]').first();
        let submit = false;
        if (!usernameSent && await username.isVisible()) {
          await username.fill(cfg.username); usernameSent = true; submit = true;
        }
        if (!passwordSent && await password.isVisible()) {
          await password.fill(cfg.password); passwordSent = true; submit = true;
        }
        if (submit) {
          const button = page.locator('button[type="submit"], input[type="submit"]').first();
          if (await button.isVisible()) await button.click();
          else if (passwordSent) await password.press('Enter');
          else await username.press('Enter');
        }
      } else { throw Error('Unexpected login origin'); }
      await page.waitForTimeout(300);
    }
    throw Error('Login did not complete');
  } finally { await page.close(); }
}

async function saveSession(context, file) {
  await mkdir(dirname(file), { recursive: true, mode: 0o700 });
  const temp = `${file}.${process.pid}.tmp`;
  try {
    await writeFile(temp, JSON.stringify(await context.storageState()), { mode: 0o600, flag: 'wx' });
    await rename(temp, file);
  } finally { await rm(temp, { force: true }); }
}

export async function execute(cfg, browserType = chromium) {
  cfg.origin = origin(cfg.origin); cfg.authOrigin = origin(cfg.authOrigin);
  const browser = await browserType.launch({ headless: true, ...(process.env.CHROMIUM_PATH ? {executablePath: process.env.CHROMIUM_PATH} : {}) });
  try {
    let storageState;
    try { storageState = JSON.parse(await readFile(cfg.sessionFile, 'utf8')); }
    catch (e) { if (e.code !== 'ENOENT') throw Error('Cannot read session file'); }
    const context = await browser.newContext({ storageState, serviceWorkers: 'block' });
    context.setDefaultTimeout(15000);
    // Only the explicitly configured service and identity provider may receive browser traffic.
    await context.route('**/*', route => {
      const target = new URL(route.request().url()).origin;
      return [cfg.origin, cfg.authOrigin].includes(target) ? route.continue() : route.abort();
    });
    let s = await session(context, cfg) || await login(context, cfg);
    await saveSession(context, cfg.sessionFile);
    if (cfg.action === 'check') return;
    if (!['start_work','stop_work','start_break','stop_break'].includes(cfg.action) || !/^[A-Za-z0-9_-]{1,128}$/.test(cfg.key)) throw Error('Invalid operation');
    for (let attempt = 0; attempt < 3; attempt++) {
      let r;
      try {
        r = await context.request.post(`${cfg.origin}/api/post-time`, {
          maxRedirects: 0, timeout: 20000,
          headers: { 'X-CSRF-Token': s.csrfToken, 'Idempotency-Key': cfg.key },
          data: { status: cfg.status, inputValue: cfg.task },
        });
      } catch { if (attempt === 2) throw Error('Booking transport failure'); continue; }
      if (r.status() === 401 || r.status() === 403) {
        s = await session(context, cfg) || await login(context, cfg);
        await saveSession(context, cfg.sessionFile);
        continue;
      }
      if (r.status() >= 500) continue;
      if (r.status() !== 200 || !r.headers()['content-type']?.includes('application/json')) throw Error('Booking rejected');
      if ((await r.json()).message !== 'Status gewechselt') throw Error('Booking not confirmed');
      return;
    }
    throw Error('Booking retries exhausted');
  } finally { await browser.close(); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    let input = '';
    for await (const part of process.stdin) input += part;
    await execute(JSON.parse(input));
    process.stdout.write(JSON.stringify({ok: true}));
  } catch {
    // Never expose library errors, URLs, credentials or tokens in subprocess output.
    process.stdout.write(JSON.stringify({ok: false})); process.exitCode = 1;
  }
}
