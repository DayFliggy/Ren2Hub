import assert from 'node:assert/strict';
import OpenAI, { toFile } from 'openai';

const client = new OpenAI({ baseURL: `${process.env.GATEWAY_SDK_URL}/v1`, apiKey: process.env.GATEWAY_SDK_KEY, maxRetries: 0 });
for (const model of ['speech-minimax', 'speech-volc-http', 'speech-volc-ws']) {
  const speech = await client.audio.speech.create({ model, input: 'hello', voice: 'alloy', response_format: 'pcm' });
  assert.deepEqual(Buffer.from(await speech.arrayBuffer()), Buffer.from([0, 1, 2, 3]));
}
const wav = Buffer.alloc(524);
wav.write('RIFF', 0); wav.writeUInt32LE(516, 4); wav.write('WAVEfmt ', 8);
wav.writeUInt32LE(16, 16); wav.writeUInt16LE(1, 20); wav.writeUInt16LE(1, 22);
wav.writeUInt32LE(24000, 24); wav.writeUInt32LE(48000, 28); wav.writeUInt16LE(2, 32); wav.writeUInt16LE(16, 34);
wav.write('data', 36); wav.writeUInt32LE(480, 40);
const file = () => toFile(wav, 'input.wav', { type: 'audio/wav' });
for (const response_format of ['json', 'text']) {
  const transcript = await client.audio.transcriptions.create({ model: 'whisper-cloudflare', file: await file(), response_format });
  assert.equal(response_format === 'json' ? transcript.text : transcript, 'hello');
}
await assert.rejects(async () => client.audio.transcriptions.create({ model: 'whisper-cloudflare', file: await file(), stream: true }), error => error instanceof OpenAI.APIError && error.status === 400);
await assert.rejects(() => client.audio.speech.create({ model: 'whisper-cloudflare', input: 'hello', voice: 'alloy' }), error => error instanceof OpenAI.APIError && error.status === 503);
await assert.rejects(async () => {
  const speech = await client.audio.speech.create({ model: 'speech-volc-ws', input: 'interrupt', voice: 'alloy', response_format: 'pcm' });
  await speech.arrayBuffer();
}, error => error instanceof TypeError || error instanceof OpenAI.APIConnectionError);
console.log('AUDIO_SDK_OK: 5 normal paths, 2 pre-send rejections, 1 binary interruption (TypeScript SDK)');
