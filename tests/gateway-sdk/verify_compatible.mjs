import assert from 'node:assert/strict';
import OpenAI from 'openai';
import Anthropic from '@anthropic-ai/sdk';
import { GoogleGenAI } from '@google/genai';

const base = process.env.GATEWAY_SDK_URL;
const key = process.env.GATEWAY_SDK_KEY;
const providers = JSON.parse(process.env.GATEWAY_COMPATIBLE_CONFIG);
const oai = new OpenAI({ baseURL: `${base}/v1`, apiKey: key, maxRetries: 0 });
const claude = new Anthropic({ baseURL: base, apiKey: key, maxRetries: 0 });
const gemini = new GoogleGenAI({ apiKey: key, httpOptions: { baseUrl: base, apiVersion: 'v1beta' } });
const failures = [];
let successes = 0;
let rejected = 0;
for (const provider of providers) {
  for (const protocol of ['chat', 'responses', 'anthropic', 'gemini']) {
    for (const mode of ['json', 'stream']) {
      const model = provider.model;
      const label = `${model}-${protocol}-${mode}`;
      const supported = provider.supported[protocol];
      try {
        if (protocol === 'chat') {
          const request = { model, messages: [{ role: 'user', content: label }], max_tokens: 32, temperature: 0.2 };
          const response = mode === 'stream' ? await oai.chat.completions.stream(request).finalChatCompletion() : await oai.chat.completions.create(request);
          assert.equal(response.choices.length, 1);
          assert.equal(response.choices[0].message.content, 'hello');
          assert.equal(response.choices[0].message.role, 'assistant');
          assert.equal(response.choices[0].finish_reason, 'stop');
          assert.equal(response.usage.prompt_tokens, 10);
          assert.equal(response.usage.completion_tokens, 2);
        } else if (protocol === 'responses') {
          const request = { model, input: label, max_output_tokens: 32, temperature: 0.2 };
          const response = mode === 'stream' ? await oai.responses.stream(request).finalResponse() : await oai.responses.create(request);
          assert.equal(response.output_text, 'hello');
          assert.equal(response.status, 'completed');
          assert.equal(response.usage.input_tokens, 10);
          assert.equal(response.usage.output_tokens, 2);
        } else if (protocol === 'anthropic') {
          const request = { model, messages: [{ role: 'user', content: label }], max_tokens: 32, temperature: 0.2 };
          const response = mode === 'stream' ? await claude.messages.stream(request).finalMessage() : await claude.messages.create(request);
          assert.equal(response.content[0].text, 'hello');
          assert.equal(response.stop_reason, 'end_turn');
          assert.equal(response.usage.input_tokens, 10);
          assert.equal(response.usage.output_tokens, 2);
        } else {
          const request = { model, contents: label, config: { maxOutputTokens: 32, temperature: 0.2 } };
          let response;
          if (mode === 'stream') {
            const chunks = [];
            for await (const chunk of await gemini.models.generateContentStream(request)) chunks.push(chunk);
            assert.equal(chunks.map(chunk => chunk.text ?? '').join(''), 'hello');
            response = chunks.at(-1);
          } else {
            response = await gemini.models.generateContent(request);
            assert.equal(response.text, 'hello');
          }
          assert.equal(response.candidates[0].finishReason, 'STOP');
          assert.equal(response.usageMetadata.promptTokenCount, 10);
          assert.equal(response.usageMetadata.candidatesTokenCount, 2);
        }
        assert.ok(supported, 'unregistered protocol was accepted');
        successes++;
        console.log(`COMPATIBLE_PATH_OK: ${label}`);
      } catch (error) {
        if (!supported && error.status === 503) {
          rejected++;
          console.log(`COMPATIBLE_PATH_REJECTED: ${label}`);
        } else {
          failures.push(`${label}: ${error}`);
        }
      }
    }
  }
}
assert.deepEqual(failures, []);
assert.equal(successes + rejected, providers.length * 8);
console.log(`COMPATIBLE_SDK_OK: ${providers.length} adapters, ${successes} JSON/SSE paths, ${rejected} pre-send rejections (TypeScript SDK)`);
