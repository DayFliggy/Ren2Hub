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
for (const provider of ['baidu', 'cohere', 'tencent', 'zhipu', 'dify', 'palm']) {
  const model = `legacy-${provider}`;
  for (const mode of ['json', 'stream']) {
    const label = `legacy-${provider}-chat-${mode}`;
    try {
      const request = { model, messages: [{ role: 'user', content: label }] };
      let response;
      if (mode === 'stream') {
        const stream = oai.chat.completions.stream(request);
        stream.on('chunk', chunk => assert.ok(chunk.choices.every(choice => choice.index === 0)));
        response = await stream.finalChatCompletion();
      } else response = await oai.chat.completions.create(request);
      assert.equal(response.choices.length, 1);
      assert.equal(response.choices[0].message.role, 'assistant');
      assert.equal(response.choices[0].message.content, 'hello');
      assert.equal(response.choices[0].finish_reason, 'stop');
      successes += 1;
      console.log(`LEGACY_PATH_OK: ${label}`);
    } catch (error) {
      failures.push(`${label}: ${error.name}: ${error.message}`);
    }
  }
  for (const protocol of ['responses', 'anthropic', 'gemini']) {
    for (const mode of ['json', 'stream']) {
      const label = `legacy-${provider}-${protocol}-${mode}`;
      try {
        if (protocol === 'responses') {
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
        throw new Error('unregistered protocol was accepted');
      } catch (error) {
        if ((error.status ?? error.code) === 503) {
          rejected += 1;
          console.log(`LEGACY_PATH_REJECTED: ${label}`);
        } else failures.push(`${label}: ${error.name}: ${error.message}`);
      }
    }
  }
  let delivered = '';
  let terminal = false;
  let failed = false;
  const label = `legacy-${provider}-chat-interrupt`;
  try {
    const stream = oai.chat.completions.stream({ model, messages: [{ role: 'user', content: label }] });
    stream.on('content', delta => { delivered += delta; });
    stream.on('chunk', chunk => { terminal ||= chunk.choices.some(choice => choice.finish_reason); });
    await stream.finalChatCompletion();
  } catch (error) {
    failed = true;
  }
  if (failed && !terminal && delivered === (provider === 'palm' ? '' : 'hello')) {
    interrupted += 1;
    console.log(`LEGACY_INTERRUPTION_OK: ${provider}, ${provider === 'palm' ? 'before_commit' : 'after_delivery'}`);
  } else failures.push(`${label}: failed=${failed}, terminal=${terminal}, delivered=${JSON.stringify(delivered)}`);
}
assert.deepEqual(failures, []);
assert.equal(successes, 12);
assert.equal(rejected, 36);
assert.equal(interrupted, 6);
console.log('LEGACY_SDK_OK: 12 Chat paths, 36 pre-send rejections, 5 delivered interruptions and 1 buffered failure (TypeScript)');
