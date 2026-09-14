import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import OpenAI from 'openai';
import { OpenAIRealtimeWS } from 'openai/realtime/ws';

const scenario = process.env.GATEWAY_REALTIME_SCENARIO;
const client = new OpenAI({ baseURL: `${process.env.GATEWAY_SDK_URL}/v1`, apiKey: process.env.GATEWAY_SDK_KEY, maxRetries: 0 });
const realtime = new OpenAIRealtimeWS({ model: 'gpt-4o-mini', options: { ca: readFileSync(process.env.GATEWAY_SDK_CA) } }, client);
const events = [];
const errors = [];
realtime.on('error', error => errors.push(error));
realtime.on('event', event => {
  events.push(event);
  if (event.type === 'session.created') {
    realtime.send({ type: 'session.update', session: {
      type: 'realtime', output_modalities: ['text'], instructions: 'isolated realtime sdk',
      tools: [{ type: 'function', name: 'square', parameters: { type: 'object', properties: { value: { type: 'integer' } } } }],
    } });
    realtime.send({ type: 'conversation.item.create', item: { type: 'message', role: 'user', content: [{ type: 'input_text', text: 'hello' }] } });
    for (const identity of ['a', 'b']) realtime.send({ type: 'response.create', response: { conversation: 'none', metadata: { identity } } });
  } else if (scenario === 'client_close' && event.type === 'response.output_text.delta') {
    realtime.close();
  } else if (event.type === 'response.done' && event.response.id === 'response-b') {
    const tool = event.response.output[0];
    assert.equal(tool.call_id, 'call-b');
    assert.equal(tool.name, 'square');
    assert.equal(tool.arguments, '{"value":7}');
    realtime.send({ type: 'conversation.item.create', item: { type: 'function_call_output', call_id: tool.call_id, output: '49' } });
  }
});
const closeCode = await new Promise((resolve, reject) => {
  const timeout = setTimeout(() => { realtime.close(); reject(new Error('Realtime SDK did not close')); }, 10000);
  realtime.socket.once('close', code => { clearTimeout(timeout); resolve(code); });
});
assert.equal(events[0].type, 'session.created');
assert.ok(events.some(event => event.type === 'session.updated' && event.session.instructions === 'isolated realtime sdk'));
const text = events.filter(event => event.type === 'response.output_text.delta');
assert.deepEqual(text.map(event => [event.response_id, event.delta]), [['response-a', 'hello']]);
const completed = events.filter(event => event.type === 'response.done').map(event => event.response);
if (scenario === 'interleaved_completed') {
  assert.equal(closeCode, 1000, 'completed Realtime session must close normally');
  assert.deepEqual(completed.map(response => response.id), ['response-a', 'response-b']);
  assert.equal(completed[0].output[0].content[0].text, 'hello');
  assert.deepEqual(completed.map(response => response.usage.total_tokens), [30, 12]);
  const argumentsEvents = events.filter(event => event.type === 'response.function_call_arguments.delta');
  assert.deepEqual(argumentsEvents.map(event => [event.response_id, event.delta]), [['response-b', '{"value":'], ['response-b', '7}']]);
  const finalArguments = events.find(event => event.type === 'response.function_call_arguments.done');
  assert.equal(finalArguments.arguments, '{"value":7}');
  assert.equal(finalArguments.call_id, 'call-b');
  assert.equal(errors.length, 0);
} else if (scenario === 'interleaved_interrupted') {
  assert.deepEqual(completed.map(response => response.id), ['response-a']);
  assert.notEqual(closeCode, 1000);
} else if (scenario === 'provider_error') {
  assert.equal(completed.length, 0);
  assert.equal(errors.length, 1);
  assert.equal(errors[0].name, 'OpenAIRealtimeError');
  assert.equal(errors[0].error.code, 'upstream_failure');
} else {
  assert.equal(completed.length, 0);
  assert.equal(errors.length, 0);
}
console.log(`REALTIME_SDK_OK: ${scenario} (TypeScript SDK event emitter)`);
