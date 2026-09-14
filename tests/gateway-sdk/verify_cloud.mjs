import assert from 'node:assert/strict';
import OpenAI from 'openai';
import Anthropic from '@anthropic-ai/sdk';
import { GoogleGenAI } from '@google/genai';

const base = process.env.GATEWAY_SDK_URL;
const key = process.env.GATEWAY_SDK_KEY;
const provider = process.env.GATEWAY_CLOUD_PROVIDER;
const phase = process.env.GATEWAY_CLOUD_PHASE ?? 'text';
const oai = new OpenAI({ baseURL: `${base}/v1`, apiKey: key, maxRetries: 0 });
const claude = new Anthropic({ baseURL: base, apiKey: key, maxRetries: 0 });
const gemini = new GoogleGenAI({ apiKey: key, httpOptions: { baseUrl: base, apiVersion: 'v1beta' } });
const failures = [];
function assertCallId(value, required = true) {
  if (provider === 'vertex_gemini') assert.ok(!required || value);
  else assert.equal(value, 'call_sdk');
}
for (const protocol of (phase === 'text' ? ['chat', 'responses', 'anthropic', 'gemini'] : [])) {
  for (const mode of ['json', 'stream']) {
    const prompt = `cloud-${protocol}-${mode}`;
    try {
      if (protocol === 'chat') {
        const request = { model: 'gpt-4o-mini', messages: [{ role: 'user', content: prompt }], max_tokens: 32, temperature: 0 };
        const response = mode === 'stream' ? await oai.chat.completions.stream(request).finalChatCompletion() : await oai.chat.completions.create(request);
        assert.equal(response.choices[0].message.content, 'hello');
        assert.equal(response.choices[0].finish_reason, 'stop');
        assert.equal(response.usage.prompt_tokens, 10);
        assert.equal(response.usage.completion_tokens, 2);
      } else if (protocol === 'responses') {
        const request = { model: 'gpt-4o-mini', input: prompt, max_output_tokens: 32, temperature: 0 };
        const response = mode === 'stream' ? await oai.responses.stream(request).finalResponse() : await oai.responses.create(request);
        assert.equal(response.output_text, 'hello');
        assert.equal(response.status, 'completed');
        assert.equal(response.usage.input_tokens, 10);
        assert.equal(response.usage.output_tokens, 2);
      } else if (protocol === 'anthropic') {
        const request = { model: 'gpt-4o-mini', messages: [{ role: 'user', content: prompt }], max_tokens: 32, temperature: 0 };
        const response = mode === 'stream' ? await claude.messages.stream(request).finalMessage() : await claude.messages.create(request);
        assert.equal(response.content[0].text, 'hello');
        assert.equal(response.stop_reason, 'end_turn');
        assert.equal(response.usage.input_tokens, 10);
        assert.equal(response.usage.output_tokens, 2);
      } else {
        const request = { model: 'gpt-4o-mini', contents: prompt, config: { maxOutputTokens: 32, temperature: 0 } };
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
      console.log(`CLOUD_PATH_OK: ${provider}/${protocol}/${mode}`);
    } catch (error) {
      failures.push(`${provider}/${protocol}/${mode}: ${error}`);
    }
  }
}
const schema = { type: 'object', properties: { value: { type: 'string' } }, required: ['value'] };
for (const protocol of (phase === 'tools_and_interrupts' ? ['chat', 'responses', 'anthropic', 'gemini'] : [])) {
  try {
    if (protocol === 'chat') {
      const options = { model: 'gpt-4o-mini', tools: [{ type: 'function', function: { name: 'echo', parameters: schema } }], max_tokens: 32, temperature: 0 };
      const final = await oai.chat.completions.stream({ ...options, messages: [{ role: 'user', content: 'use_tool' }] }).finalChatCompletion();
      const call = final.choices[0].message.tool_calls[0];
      assertCallId(call.id);
      assert.equal(call.function.name, 'echo');
      assert.deepEqual(JSON.parse(call.function.arguments), { value: 'example' });
      const response = await oai.chat.completions.create({ ...options, messages: [{ role: 'user', content: 'use_tool' }, final.choices[0].message, { role: 'tool', tool_call_id: call.id, content: JSON.stringify({ result: 'done' }) }] });
      assert.equal(response.choices[0].message.content, 'hello');
    } else if (protocol === 'responses') {
      const options = { model: 'gpt-4o-mini', tools: [{ type: 'function', name: 'echo', parameters: schema }], max_output_tokens: 32, temperature: 0 };
      const final = await oai.responses.stream({ ...options, input: 'use_tool' }).finalResponse();
      const call = final.output[0];
      assert.equal(call.type, 'function_call');
      assertCallId(call.call_id);
      assert.equal(call.name, 'echo');
      assert.deepEqual(JSON.parse(call.arguments), { value: 'example' });
      const response = await oai.responses.create({ ...options, input: [{ role: 'user', content: 'use_tool' }, call, { type: 'function_call_output', call_id: call.call_id, output: JSON.stringify({ result: 'done' }) }] });
      assert.equal(response.output_text, 'hello');
    } else if (protocol === 'anthropic') {
      const options = { model: 'gpt-4o-mini', tools: [{ name: 'echo', input_schema: schema }], max_tokens: 32, temperature: 0 };
      const final = await claude.messages.stream({ ...options, messages: [{ role: 'user', content: 'use_tool' }] }).finalMessage();
      const call = final.content[0];
      assert.equal(call.type, 'tool_use');
      assertCallId(call.id);
      assert.equal(call.name, 'echo');
      assert.deepEqual(call.input, { value: 'example' });
      const response = await claude.messages.create({ ...options, messages: [{ role: 'user', content: 'use_tool' }, { role: 'assistant', content: final.content }, { role: 'user', content: [{ type: 'tool_result', tool_use_id: call.id, content: JSON.stringify({ result: 'done' }) }] }] });
      assert.equal(response.content[0].text, 'hello');
    } else {
      const config = { maxOutputTokens: 32, temperature: 0, tools: [{ functionDeclarations: [{ name: 'echo', parameters: schema }] }] };
      let call;
      for await (const chunk of await gemini.models.generateContentStream({ model: 'gpt-4o-mini', contents: 'use_tool', config })) {
        for (const candidate of chunk.functionCalls ?? []) if (candidate.args?.value) call = candidate;
      }
      assertCallId(call.id, false);
      assert.equal(call.name, 'echo');
      assert.deepEqual(call.args, { value: 'example' });
      const response = await gemini.models.generateContent({ model: 'gpt-4o-mini', config, contents: [{ role: 'user', parts: [{ text: 'use_tool' }] }, { role: 'model', parts: [{ functionCall: call }] }, { role: 'user', parts: [{ functionResponse: { id: call.id, name: call.name, response: { result: 'done' } } }] }] });
      assert.equal(response.text, 'hello');
    }
    console.log(`CLOUD_TOOL_OK: ${provider}/${protocol}`);
  } catch (error) {
    failures.push(`${provider}/${protocol}/tool: ${error}`);
  }
}
const parallelValues = { echo_zero: 0, echo_false: false };
const parallelFunctions = [['echo_zero', 'integer'], ['echo_false', 'boolean']].map(([name, type]) => ({
  name, parameters: { type: 'object', properties: { value: { type } }, required: ['value'] },
}));
function assertParallelCalls(calls, nativeGemini = false) {
  assert.equal(calls.length, 2);
  assert.deepEqual(calls.map(call => call.name).sort(), Object.keys(parallelValues).sort());
  if (provider !== 'vertex_gemini' || !nativeGemini) {
    assert.equal(new Set(calls.map(call => call.id)).size, 2);
    assert.ok(calls.every(call => call.id));
  }
  for (const call of calls) {
    assert.deepEqual(call.args, { value: parallelValues[call.name] });
    if (provider !== 'vertex_gemini') assert.equal(call.id, call.name.replace('echo_', 'call_'));
  }
}
for (const protocol of (phase === 'parallel_tools' ? ['chat', 'responses', 'anthropic', 'gemini'] : [])) {
  try {
    if (protocol === 'chat') {
      const options = { model: 'gpt-4o-mini', tools: parallelFunctions.map(fn => ({ type: 'function', function: fn })), max_tokens: 32, temperature: 0 };
      const stream = oai.chat.completions.stream({ ...options, messages: [{ role: 'user', content: 'use_parallel_tools' }] });
      const endings = [];
      stream.on('chunk', chunk => endings.push(...chunk.choices.map(choice => choice.finish_reason).filter(Boolean)));
      const final = await stream.finalChatCompletion();
      assert.deepEqual(endings, ['tool_calls']);
      assert.equal(final.usage.completion_tokens, 4);
      const calls = final.choices[0].message.tool_calls;
      assertParallelCalls(calls.map(call => ({ id: call.id, name: call.function.name, args: JSON.parse(call.function.arguments) })));
      const results = calls.toReversed().map(call => ({ role: 'tool', tool_call_id: call.id, content: JSON.stringify({ result: parallelValues[call.function.name] }) }));
      const response = await oai.chat.completions.create({ ...options, messages: [{ role: 'user', content: 'use_parallel_tools' }, final.choices[0].message, ...results] });
      assert.equal(response.choices[0].message.content, 'hello');
    } else if (protocol === 'responses') {
      const options = { model: 'gpt-4o-mini', tools: parallelFunctions.map(fn => ({ type: 'function', ...fn })), max_output_tokens: 32, temperature: 0 };
      const stream = oai.responses.stream({ ...options, input: 'use_parallel_tools' });
      const endings = [];
      stream.on('response.completed', event => endings.push(event.type));
      const final = await stream.finalResponse();
      assert.deepEqual(endings, ['response.completed']);
      assert.equal(final.status, 'completed');
      assert.equal(final.usage.output_tokens, 4);
      const calls = final.output.filter(item => item.type === 'function_call');
      assertParallelCalls(calls.map(call => ({ id: call.call_id, name: call.name, args: JSON.parse(call.arguments) })));
      const results = calls.toReversed().map(call => ({ type: 'function_call_output', call_id: call.call_id, output: JSON.stringify({ result: parallelValues[call.name] }) }));
      const response = await oai.responses.create({ ...options, input: [{ role: 'user', content: 'use_parallel_tools' }, ...calls, ...results] });
      assert.equal(response.output_text, 'hello');
    } else if (protocol === 'anthropic') {
      const options = { model: 'gpt-4o-mini', tools: parallelFunctions.map(fn => ({ name: fn.name, input_schema: fn.parameters })), max_tokens: 32, temperature: 0 };
      const stream = claude.messages.stream({ ...options, messages: [{ role: 'user', content: 'use_parallel_tools' }] });
      const endings = [];
      stream.on('streamEvent', event => { if (event.type === 'message_stop') endings.push(event.type); });
      const final = await stream.finalMessage();
      assert.deepEqual(endings, ['message_stop']);
      assert.equal(final.stop_reason, 'tool_use');
      assert.equal(final.usage.output_tokens, 4);
      const calls = final.content.filter(block => block.type === 'tool_use');
      assertParallelCalls(calls.map(call => ({ id: call.id, name: call.name, args: call.input })));
      const results = calls.toReversed().map(call => ({ type: 'tool_result', tool_use_id: call.id, content: JSON.stringify({ result: parallelValues[call.name] }) }));
      const response = await claude.messages.create({ ...options, messages: [{ role: 'user', content: 'use_parallel_tools' }, { role: 'assistant', content: final.content }, { role: 'user', content: results }] });
      assert.equal(response.content[0].text, 'hello');
    } else {
      const config = { maxOutputTokens: 32, temperature: 0, tools: [{ functionDeclarations: parallelFunctions }] };
      const chunks = [];
      for await (const chunk of await gemini.models.generateContentStream({ model: 'gpt-4o-mini', contents: 'use_parallel_tools', config })) chunks.push(chunk);
      const calls = chunks.flatMap(chunk => chunk.functionCalls ?? []);
      assertParallelCalls(calls, true);
      assert.equal(chunks.at(-1).usageMetadata.candidatesTokenCount, 4);
      const results = calls.toReversed().map(call => ({ functionResponse: { id: call.id, name: call.name, response: { result: parallelValues[call.name] } } }));
      const response = await gemini.models.generateContent({ model: 'gpt-4o-mini', config, contents: [{ role: 'user', parts: [{ text: 'use_parallel_tools' }] },
        { role: 'model', parts: calls.map(call => ({ functionCall: call })) }, { role: 'user', parts: results }] });
      assert.equal(response.text, 'hello');
    }
    console.log(`CLOUD_PARALLEL_TOOL_OK: ${provider}/${protocol}, two calls, arguments 0/false, reversed results`);
  } catch (error) {
    failures.push(`${provider}/${protocol}/parallel tools: ${error}`);
  }
}
for (const protocol of (phase === 'tools_and_interrupts' ? ['chat', 'responses', 'anthropic', 'gemini'] : [])) {
  const delivered = [];
  let failed = false;
  try {
    if (protocol === 'chat') {
      const stream = oai.chat.completions.stream({ model: 'gpt-4o-mini', messages: [{ role: 'user', content: 'interrupt' }], max_tokens: 32, temperature: 0 });
      stream.on('content.delta', event => delivered.push(event.delta));
      stream.on('chunk', chunk => {
        if (chunk.choices.some(choice => choice.finish_reason)) failures.push(`${provider}/${protocol}/interruption: unexpected finish_reason`);
      });
      await stream.finalChatCompletion();
    } else if (protocol === 'responses') {
      const stream = oai.responses.stream({ model: 'gpt-4o-mini', input: 'interrupt', max_output_tokens: 32, temperature: 0 });
      stream.on('response.output_text.delta', event => delivered.push(event.delta));
      stream.on('response.completed', () => failures.push(`${provider}/${protocol}/interruption: unexpected response.completed`));
      const final = await stream.finalResponse();
      failed = ['failed', 'incomplete'].includes(final.status);
    } else if (protocol === 'anthropic') {
      const stream = claude.messages.stream({ model: 'gpt-4o-mini', messages: [{ role: 'user', content: 'interrupt' }], max_tokens: 32, temperature: 0 });
      stream.on('streamEvent', event => {
        if (event.type === 'content_block_delta' && event.delta.type === 'text_delta') delivered.push(event.delta.text);
        if (event.type === 'message_stop') failures.push(`${provider}/${protocol}/interruption: unexpected message_stop`);
      });
      await stream.finalMessage();
    } else {
      for await (const chunk of await gemini.models.generateContentStream({ model: 'gpt-4o-mini', contents: 'interrupt', config: { maxOutputTokens: 32, temperature: 0 } })) {
        delivered.push(chunk.text ?? '');
        assert.ok(!chunk.candidates?.some(candidate => candidate.finishReason));
      }
    }
  } catch (error) {
    if (error instanceof assert.AssertionError) failures.push(`${provider}/${protocol}/interruption terminal: ${error}`);
    else failed = true;
  }
  if (!failed || delivered.join('') !== 'hello') failures.push(`${provider}/${protocol}/interruption: failed=${failed}, delivered=${JSON.stringify(delivered)}`);
  else console.log(`CLOUD_INTERRUPTION_OK: ${provider}/${protocol}, delivered=hello`);
  try {
    const response = await oai.chat.completions.create({ model: 'gpt-4o-mini', messages: [{ role: 'user', content: 'health_recovery' }], max_tokens: 32, temperature: 0 });
    assert.equal(response.choices[0].message.content, 'hello');
  } catch (error) {
    failures.push(`${provider}/${protocol}/health recovery: ${error}`);
  }
}
assert.deepEqual(failures, []);
const summaries = { text: '8 text paths', tools_and_interrupts: '4 tool round trips, 4 delivered interruptions', parallel_tools: '4 parallel tool round trips' };
console.log(`CLOUD_SDK_OK: ${provider}, ${summaries[phase]} (TypeScript SDK)`);
