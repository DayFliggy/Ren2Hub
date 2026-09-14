import assert from 'node:assert/strict';
import OpenAI from 'openai';
import Anthropic from '@anthropic-ai/sdk';
import { GoogleGenAI } from '@google/genai';

const base = process.env.GATEWAY_SDK_URL;
const key = process.env.GATEWAY_SDK_KEY;
const oai = new OpenAI({ baseURL: `${base}/v1`, apiKey: key, maxRetries: 0 });
const claude = new Anthropic({ baseURL: base, apiKey: key, maxRetries: 0 });
const gemini = new GoogleGenAI({ apiKey: key, httpOptions: { baseUrl: base, apiVersion: 'v1beta' } });
const failures = [];
let successes = 0;
let rejected = 0;
let interrupted = 0;
for (const provider of ['nova', 'coze', 'xunfei']) {
  const model = `special-${provider}`;
  for (const protocol of ['chat', 'responses', 'anthropic', 'gemini']) {
    for (const mode of ['json', 'stream']) {
      const label = `special-${provider}-${protocol}-${mode}`;
      const supported = protocol === 'chat' && (provider !== 'nova' || mode === 'json');
      try {
        if (protocol === 'chat') {
          const request = { model, messages: [{ role: 'user', content: label }] };
          let response;
          if (mode === 'stream') {
            let deltas = 0;
            let roles = 0;
            const stream = oai.chat.completions.stream(request);
            stream.on('chunk', chunk => {
              for (const choice of chunk.choices) {
                assert.equal(choice.index, 0);
                if (choice.delta.role) roles += 1;
                if (choice.delta.content) deltas += 1;
              }
            });
            response = await stream.finalChatCompletion();
            assert.equal(roles, 1);
            assert.ok(deltas >= 2);
          } else response = await oai.chat.completions.create(request);
          assert.ok(supported, 'unsupported path accepted');
          assert.equal(response.choices.length, 1);
          assert.equal(response.choices[0].message.role, 'assistant');
          assert.equal(response.choices[0].message.content, 'hello');
          assert.equal(response.choices[0].finish_reason, 'stop');
        } else if (protocol === 'responses') {
          const request = { model, input: label, max_output_tokens: 32 };
          if (mode === 'stream') await oai.responses.stream(request).finalResponse();
          else await oai.responses.create(request);
        } else if (protocol === 'anthropic') {
          const request = { model, messages: [{ role: 'user', content: label }], max_tokens: 32 };
          if (mode === 'stream') await claude.messages.stream(request).finalMessage();
          else await claude.messages.create(request);
        } else {
          const request = { model, contents: label, config: { maxOutputTokens: 32 } };
          if (mode === 'stream') {
            for await (const chunk of await gemini.models.generateContentStream(request)) assert.ok(chunk);
          } else await gemini.models.generateContent(request);
        }
        assert.ok(supported, 'unsupported path accepted');
        successes += 1;
        console.log(`SPECIAL_PATH_OK: ${label}`);
      } catch (error) {
        const expectedStatus = provider === 'nova' && protocol === 'chat' && mode === 'stream' ? 400 : 503;
        if (!supported && (error.status ?? error.code) === expectedStatus) {
          rejected += 1;
          console.log(`SPECIAL_PATH_REJECTED: ${label}`);
        } else failures.push(`${label}: ${error.name}: ${error.message}`);
      }
    }
  }
  if (provider !== 'nova') {
    let delivered = '';
    let terminal = false;
    let failed = false;
    const label = `special-${provider}-chat-interrupt`;
    try {
      const stream = oai.chat.completions.stream({ model, messages: [{ role: 'user', content: label }] });
      stream.on('content', delta => { delivered += delta; });
      stream.on('chunk', chunk => { terminal ||= chunk.choices.some(choice => choice.finish_reason); });
      await stream.finalChatCompletion();
    } catch (error) {
      failed = true;
    }
    if (failed && !terminal && delivered === 'hello') {
      interrupted += 1;
      console.log(`SPECIAL_INTERRUPTION_OK: ${provider}`);
    } else failures.push(`${label}: failed=${failed}, terminal=${terminal}, delivered=${JSON.stringify(delivered)}`);
  }
}
try {
  await oai.chat.completions.create({ model: 'special-coze-poll-failure', messages: [{ role: 'user', content: 'special-coze-chat-poll_failure' }] });
  failures.push('Coze status query failure was accepted');
} catch (error) {
  if (error.status !== 502) failures.push(`Coze status query: ${error.name}: ${error.message}`);
  else console.log('SPECIAL_POLL_FAILURE_OK: accepted Coze chat was not replayed');
}
assert.deepEqual(failures, []);
assert.equal(successes, 5);
assert.equal(rejected, 19);
assert.equal(interrupted, 2);
console.log('SPECIAL_SDK_OK: 5 Chat paths, 19 pre-send rejections, 2 delivered interruptions, 1 accepted poll failure (TypeScript)');
