import assert from 'node:assert/strict';
import OpenAI from 'openai';
import Anthropic from '@anthropic-ai/sdk';
import { GoogleGenAI } from '@google/genai';

const base = process.env.GATEWAY_SDK_URL;
const key = process.env.GATEWAY_SDK_KEY;
const oai = new OpenAI({ baseURL: `${base}/v1`, apiKey: key, maxRetries: 0 });
const foreign = new OpenAI({ baseURL: `${base}/v1`, apiKey: process.env.GATEWAY_SDK_FOREIGN_KEY, maxRetries: 0 });
const claude = new Anthropic({ baseURL: base, apiKey: key, maxRetries: 0 });
const gemini = new GoogleGenAI({ apiKey: key, httpOptions: { baseUrl: base, apiVersion: 'v1beta', retryOptions: { attempts: 1 } } });

async function control(action) {
  const response = await fetch(`${process.env.GATEWAY_CODEX_CONTROL}/${action}`, { method: 'POST' });
  assert.equal(response.status, 204, await response.text());
}

async function streamResponse(options) {
  const stream = oai.responses.stream(options);
  const endings = [];
  stream.on('response.completed', () => endings.push('response.completed'));
  const response = await stream.finalResponse();
  assert.deepEqual(endings, ['response.completed']);
  assert.equal(response.status, 'completed');
  assert.equal(response.output_text, 'hello');
  assert.equal(response.usage.output_tokens, 2);
  return response;
}

const stored = await oai.responses.create({ model: 'codex-fixture', input: 'codex-basic-json' });
assert.equal(stored.id, 'resp_codex-basic-json');
assert.equal(stored.output_text, 'hello');
assert.equal(stored.usage.input_tokens, 10);
await streamResponse({ model: 'codex-fixture', input: 'codex-basic-stream' });
for (const streaming of [false, true]) {
  const chat = { model: 'codex-fixture', messages: [{ role: 'user', content: 'rejected-chat' }] };
  await assert.rejects(async () => streaming ? await oai.chat.completions.stream(chat).finalChatCompletion() : await oai.chat.completions.create(chat), { status: 503 });
  const message = { model: 'codex-fixture', messages: [{ role: 'user', content: 'rejected-anthropic' }], max_tokens: 32 };
  await assert.rejects(async () => streaming ? await claude.messages.stream(message).finalMessage() : await claude.messages.create(message), { status: 503 });
  await assert.rejects(async () => {
    const content = { model: 'codex-fixture', contents: 'rejected-gemini' };
    if (streaming) {
      for await (const event of await gemini.models.generateContentStream(content)) assert.fail(`unexpected event: ${event}`);
    } else await gemini.models.generateContent(content);
  }, error => error.status === 503 || error.code === 503);
}
await assert.rejects(oai.responses.create({ model: 'codex-fixture', previous_response_id: 'unknown', input: 'unknown-state' }), { status: 400 });
await assert.rejects(foreign.responses.create({ model: 'codex-fixture', previous_response_id: stored.id, input: 'foreign-state' }), { status: 400 });
await control('reorder');
assert.equal((await oai.responses.create({ model: 'codex-fixture', previous_response_id: stored.id, input: 'codex-continue-json' })).output_text, 'hello');
await streamResponse({ model: 'codex-fixture', previous_response_id: stored.id, input: 'codex-continue-stream' });
for (const action of ['disable', 'replace']) {
  await control(action);
  await assert.rejects(oai.responses.create({ model: 'codex-fixture', previous_response_id: stored.id, input: 'unavailable-state' }), { status: 503 });
}
await assert.rejects(oai.responses.create({ model: 'codex-errors', input: 'codex-error-rejected' }), { status: 400 });
const delivered = [];
let completed = false;
let failed = false;
const stream = oai.responses.stream({ model: 'codex-errors', input: 'codex-error-interrupt' });
stream.on('response.output_text.delta', event => delivered.push(event.delta));
stream.on('response.completed', () => { completed = true; });
stream.on('response.failed', () => { failed = true; });
stream.on('response.incomplete', () => { failed = true; });
try {
  const response = await stream.finalResponse();
  failed ||= ['failed', 'incomplete'].includes(response.status);
} catch {
  failed = true;
}
assert.equal(delivered.join(''), 'hello');
assert.equal(completed, false);
assert.equal(failed, true);
console.log('CODEX_SDK_OK: 2 Responses paths, 6 protocol rejections, 2 owner rejections, 2 reordered continuations, 2 unavailable owners, 1 provider error, 1 delivered interruption (TypeScript SDK)');
