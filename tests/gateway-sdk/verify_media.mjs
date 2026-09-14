import assert from 'node:assert/strict';
import OpenAI, { toFile } from 'openai';

const client = new OpenAI({ baseURL: `${process.env.GATEWAY_SDK_URL}/v1`, apiKey: process.env.GATEWAY_SDK_KEY, maxRetries: 0 });
const model = 'gpt-4o-mini';
for (const encoding_format of ['float', 'base64']) {
  const vectors = await client.embeddings.create({ model, input: ['hello', 'world'], encoding_format });
  assert.equal(vectors.data.length, 2);
  assert.equal(vectors.data[0].index, 0);
}
const image = await client.images.generate({ model, prompt: 'house', n: 1 });
assert.equal(image.data[0].b64_json, 'AAECAw==');
const images = [];
for await (const event of await client.images.generate({ model, prompt: 'house', n: 1, stream: true })) images.push(event);
assert.deepEqual(images.map(event => event.type), ['image_generation.partial_image', 'image_generation.completed']);
assert.equal(images[1].b64_json, 'AAECAw==');
assert.equal(images[1].usage.total_tokens, 30);

const speech = await client.audio.speech.create({ model, input: 'hello', voice: 'alloy', response_format: 'pcm' });
assert.deepEqual(Buffer.from(await speech.arrayBuffer()), Buffer.from([0, 1, 2, 3]));
const wav = Buffer.alloc(524);
wav.write('RIFF', 0); wav.writeUInt32LE(516, 4); wav.write('WAVEfmt ', 8);
wav.writeUInt32LE(16, 16); wav.writeUInt16LE(1, 20); wav.writeUInt16LE(1, 22);
wav.writeUInt32LE(24000, 24); wav.writeUInt32LE(48000, 28); wav.writeUInt16LE(2, 32); wav.writeUInt16LE(16, 34);
wav.write('data', 36); wav.writeUInt32LE(480, 40);
for (const stream of [false, true]) {
  const transcript = await client.audio.transcriptions.create({ model, file: await toFile(wav, 'input.wav', { type: 'audio/wav' }), stream });
  if (stream) {
    const events = [];
    for await (const event of transcript) events.push(event);
    assert.deepEqual(events.map(event => event.type), ['transcript.text.delta', 'transcript.text.done']);
    assert.equal(events[1].text, 'hello');
  } else assert.equal(transcript.text, 'hello');
}
const partial = [];
await assert.rejects(async () => {
  for await (const event of await client.images.generate({ model, prompt: 'interrupt', n: 1, stream: true })) partial.push(event);
}, OpenAI.APIError);
assert.deepEqual(partial.map(event => event.type), ['image_generation.partial_image']);
console.log('MEDIA_SDK_OK: 7 normal paths and image interruption (TypeScript SDK)');
