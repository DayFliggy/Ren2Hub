#!/usr/bin/env node
// 官方 Node SDK 单次回环检查；凭据仅从 stdin 读取。
import { readFile, stat } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { dirname, isAbsolute, join } from 'node:path';
import { pathToFileURL } from 'node:url';

const TEXT = 'local answer';
const USAGE = { input_tokens: 10, output_tokens: 2, total_tokens: 12 };
const CASES = ['chat_json', 'chat_sse', 'responses_json', 'responses_sse'];
class ContractError extends Error {}
function require(condition, code) { if (!condition) throw new ContractError(code); }
function usage(value, chat = false) {
  require(value && (chat ? value.prompt_tokens : value.input_tokens) === 10
    && (chat ? value.completion_tokens : value.output_tokens) === 2 && value.total_tokens === 12, 'sdk_usage_mismatch');
}
function responseText(response) {
  return response.output.filter(item => item.type === 'message')
    .flatMap(item => item.content).filter(part => part.type === 'output_text').map(part => part.text).join('');
}

async function run(config) {
  require(config && CASES.includes(config.case), 'sdk_input_invalid');
  const target = new URL(config.base_url);
  require(target.protocol === 'http:' && target.hostname === '127.0.0.1' && target.port
    && !target.username && !target.password && target.pathname === '/v1' && !target.search && !target.hash,
  'sdk_explicit_loopback_required');
  require(typeof config.api_key === 'string' && config.api_key && typeof config.model === 'string' && config.model,
    'sdk_credentials_or_model_missing');
  const modulePath = process.env.GATEWAY_SDK_NODE_MODULE;
  require(modulePath && isAbsolute(modulePath), 'explicit_sdk_node_module_required');
  const directory = (await stat(modulePath)).isDirectory() ? modulePath : dirname(modulePath);
  const entry = (await stat(modulePath)).isDirectory() ? join(modulePath, 'index.mjs') : modulePath;
  const packageMetadata = await readFile(join(directory, 'package.json'));
  const metadata = JSON.parse(packageMetadata.toString('utf8'));
  require(metadata.name === 'openai' && typeof metadata.version === 'string', 'official_sdk_module_required');
  const { default: OpenAI } = await import(pathToFileURL(entry).href);
  let requests = 0;
  const client = new OpenAI({ apiKey: config.api_key, baseURL: config.base_url, maxRetries: 0, timeout: 20000,
    fetch: (input, init) => {
      const url = new URL(typeof input === 'string' || input instanceof URL ? input : input.url);
      require(url.origin === target.origin && ['/v1/chat/completions', '/v1/responses'].includes(url.pathname)
        && !url.search && !url.hash, 'sdk_non_loopback_request_rejected');
      requests += 1;
      return globalThis.fetch(input, { ...init, redirect: 'manual' });
    },
  });
  let terminal;
  if (config.case.startsWith('chat_')) {
    const options = { model: config.model, messages: [{ role: 'user', content: 'local acceptance' }], max_tokens: 8 };
    if (config.case === 'chat_json') {
      const result = await client.chat.completions.create(options);
      require(result.model === config.model && result.choices.length === 1, 'sdk_chat_identity_mismatch');
      const choice = result.choices[0];
      require(choice.message.content === TEXT && choice.message.role === 'assistant' && choice.finish_reason === 'stop',
        'sdk_chat_result_mismatch');
      usage(result.usage, true);
    } else {
      let text = '', usages = 0;
      const finishes = [];
      const stream = await client.chat.completions.create({ ...options, stream: true, stream_options: { include_usage: true } });
      for await (const chunk of stream) {
        require(chunk.model === config.model, 'sdk_chat_stream_identity_mismatch');
        for (const choice of chunk.choices) {
          require(choice.index === 0, 'sdk_chat_choice_mismatch');
          if (choice.delta.content) {
            require(finishes.length === 0, 'sdk_chat_content_after_terminal');
            text += choice.delta.content;
          }
          if (choice.finish_reason != null) finishes.push(choice.finish_reason);
        }
        if (chunk.usage != null) { usage(chunk.usage, true); usages += 1; }
      }
      require(text === TEXT && finishes.length === 1 && finishes[0] === 'stop' && usages > 0,
        'sdk_chat_stream_terminal_or_usage_missing');
    }
    terminal = 'stop';
  } else {
    const options = { model: config.model, input: 'local acceptance', max_output_tokens: 32 };
    let response;
    if (config.case === 'responses_json') {
      response = await client.responses.create(options);
      require(response.output_text === TEXT, 'sdk_responses_output_text_mismatch');
    } else {
      let text = '';
      const events = [];
      const stream = await client.responses.create({ ...options, stream: true });
      for await (const event of stream) {
        require(!response, 'sdk_responses_event_after_terminal');
        events.push(event.type);
        require(!['response.failed', 'response.incomplete', 'error'].includes(event.type), 'sdk_responses_failed');
        if (event.type === 'response.output_text.delta') text += event.delta;
        if (event.type === 'response.completed') response = event.response;
      }
      require(events[0] === 'response.created' && events.at(-1) === 'response.completed' && text === TEXT,
        'sdk_responses_stream_terminal_or_text_missing');
    }
    require(response && response.model === config.model && response.status === 'completed'
      && responseText(response) === TEXT && response.error == null, 'sdk_responses_result_mismatch');
    usage(response.usage);
    terminal = 'completed';
  }
  require(requests === 1, 'sdk_request_replayed');
  return { success: true, sdk: 'node', sdk_version: metadata.version, runtime_version: process.version,
    package_metadata_sha256: createHash('sha256').update(packageMetadata).digest('hex'),
    case: config.case, requests, max_retries: 0, text: TEXT, usage: USAGE, terminal };
}

let result;
try {
  const chunks = [];
  let size = 0;
  for await (const chunk of process.stdin) {
    size += chunk.length;
    require(size <= 16384, 'sdk_input_too_large');
    chunks.push(chunk);
  }
  result = await run(JSON.parse(Buffer.concat(chunks).toString('utf8')));
} catch (error) {
  result = { success: false, error_code: error instanceof ContractError ? error.message : 'sdk_call_failed',
    error_type: error?.constructor?.name || 'Error' };
}
console.log(JSON.stringify(result));
process.exitCode = result.success ? 0 : 1;
