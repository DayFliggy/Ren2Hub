import assert from 'node:assert/strict';
import fs from 'node:fs';
import OpenAI from 'openai';
import Anthropic from '@anthropic-ai/sdk';
import { GoogleGenAI } from '@google/genai';

const base = process.env.GATEWAY_SDK_URL;
const cases = JSON.parse(fs.readFileSync(process.env.GATEWAY_SEMANTIC_CASES, 'utf8'));
const clients = {};
for (const [name, variable] of [['safe', 'GATEWAY_SDK_KEY'], ['strict', 'GATEWAY_SEMANTIC_STRICT_KEY'], ['foreign', 'GATEWAY_SDK_FOREIGN_KEY']]) {
  const key = process.env[variable];
  clients[name] = [new OpenAI({ baseURL: `${base}/v1`, apiKey: key, maxRetries: 0 }), new Anthropic({ baseURL: base, apiKey: key, maxRetries: 0 }), new GoogleGenAI({ apiKey: key, httpOptions: { baseUrl: base, apiVersion: 'v1beta' } })];
}
const state = new Map();
const counts = {};
const observe = async () => (await fetch(process.env.GATEWAY_SEMANTIC_OBSERVER)).json();

function quotaAfterCharge(value, charge) {
  assert.equal(typeof value, 'string', 'quota must use the decimal string wire contract');
  assert.match(value, /^(0|-?[1-9]\d*)$/);
  return (BigInt(value) - charge).toString();
}

function requestFor(test) {
  const request = structuredClone(test.body);
  const { protocol, feature, action, label } = test;
  if (!['state', 'private_file'].includes(feature) || action === 'seed') return request;
  const unknown = action === 'unknown';
  if (protocol === 'chat') {
    request.messages = [{ role: 'user', content: [{ type: 'text', text: label }, { type: 'file', file: { file_id: unknown ? 'file-unknown' : 'file-semantic-local' } }] }];
  } else if (protocol === 'responses') {
    const item = structuredClone(state.get(`${protocol}/${feature}`).output[0]);
    if (unknown) { item.encrypted_content = 'semantic-unknown'; item.id = 'reason-unknown'; }
    request.input = [item, { role: 'user', content: label }];
  } else if (protocol === 'anthropic') {
    const block = structuredClone(state.get(`${protocol}/${feature}`).content[0]);
    if (unknown) block.signature = 'unknown-signature';
    request.messages = [{ role: 'assistant', content: [block] }, { role: 'user', content: label }];
  } else {
    const part = structuredClone(state.get(`${protocol}/${feature}`).candidates[0].content.parts[0]);
    if (unknown) {
      if (feature === 'private_file') part.fileData.fileUri = 'https://generativelanguage.googleapis.com/v1beta/files/unknown-semantic';
      else part.thoughtSignature = Buffer.from('unknown-signature').toString('base64');
    }
    request.contents = [{ role: 'model', parts: [part] }, { role: 'user', parts: [{ text: label }] }];
  }
  return request;
}

async function call(test) {
  const [oai, claude, gemini] = clients[test.foreign ? 'foreign' : test.policy];
  const request = requestFor(test);
  let response, text, usage;
  if (test.protocol === 'chat') {
    response = test.stream ? await oai.chat.completions.stream(request).finalChatCompletion() : await oai.chat.completions.create(request);
    assert.equal(response.choices[0].finish_reason, 'stop');
    text = response.choices[0].message.content;
    usage = [response.usage.prompt_tokens, response.usage.completion_tokens];
  } else if (test.protocol === 'responses') {
    response = test.stream ? await oai.responses.stream(request).finalResponse() : await oai.responses.create(request);
    assert.equal(response.status, 'completed');
    text = response.output_text;
    usage = [response.usage.input_tokens, response.usage.output_tokens];
  } else if (test.protocol === 'anthropic') {
    response = test.stream ? await claude.messages.stream(request).finalMessage() : await claude.messages.create(request);
    assert.equal(response.stop_reason, 'end_turn');
    text = response.content.filter(block => block.type === 'text').map(block => block.text).join('');
    usage = [response.usage.input_tokens, response.usage.output_tokens];
  } else {
    if (test.stream) {
      const chunks = [];
      for await (const chunk of await gemini.models.generateContentStream(request)) chunks.push(chunk);
      text = chunks.map(chunk => chunk.text ?? '').join('');
      response = chunks.at(-1);
    } else {
      response = await gemini.models.generateContent(request);
      text = response.text;
    }
    assert.equal(response.candidates[0].finishReason, 'STOP');
    usage = [response.usageMetadata.promptTokenCount, response.usageMetadata.candidatesTokenCount];
  }
  assert.deepEqual(usage, [10, 2]);
  if (test.feature === 'structured') assert.deepEqual(JSON.parse(text), { value: 'hello' });
  else assert.equal(text, 'hello');
  if (test.action === 'seed') state.set(`${test.protocol}/${test.feature}`, response);
}

for (const test of cases) {
  const before = await observe();
  try {
    await call(test);
    assert.ok(!test.reject, 'rejected semantic request succeeded');
  } catch (error) {
    assert.ok(test.reject && (error.status ?? error.code) === 400, `${test.label}: ${error.stack}`);
  }
  const charge = test.reject ? 0n : 500n;
  const expected = { sends: before.sends + Number(!test.reject), quota: quotaAfterCharge(before.quota, charge), token: quotaAfterCharge(before.token, charge), logs: before.logs + Number(!test.reject), billing_sessions: before.billing_sessions + Number(!test.reject) };
  const deadline = Date.now() + 2000;
  let after = await observe();
  while (!Object.keys(expected).every(key => expected[key] === after[key]) && Date.now() < deadline) {
    await new Promise(resolve => setTimeout(resolve, 10));
    after = await observe();
  }
  assert.deepEqual(after, expected, test.label);
  counts[test.protocol] ??= { success: 0, rejected: 0 };
  counts[test.protocol][test.reject ? 'rejected' : 'success']++;
  console.log(`SEMANTIC_PATH_OK: ${test.label}`);
}
console.log(`SEMANTIC_SDK_OK: TypeScript ${JSON.stringify(counts)}`);
