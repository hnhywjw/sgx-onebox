import { access, readFile } from 'node:fs/promises';
import path from 'node:path';

const baseUrl = process.env.SMOKE_BASE_URL || 'http://localhost:5173';
const distIndex = path.join(process.cwd(), 'dist', 'index.html');

async function assertBuildArtifacts() {
  await access(distIndex);
  const html = await readFile(distIndex, 'utf8');
  if (!html.includes('<div id="root"></div>')) {
    throw new Error('dist/index.html missing root container');
  }
}

async function assertHttp(url, expected = 200) {
  const res = await fetch(url);
  if (res.status !== expected) {
    throw new Error(`${url} expected ${expected}, got ${res.status}`);
  }
  return res;
}

await assertBuildArtifacts();
await assertHttp(`${baseUrl}/`);
await assertHttp(`${baseUrl}/api/v1/health`);
console.log('smoke:e2e ok');
