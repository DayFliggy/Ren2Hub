import assert from 'node:assert/strict';
import OpenAI from 'openai';
import Anthropic from '@anthropic-ai/sdk';
import { GoogleGenAI } from '@google/genai';

const base = process.env.GATEWAY_SDK_URL;
const key = process.env.GATEWAY_SDK_KEY;
const oai = new OpenAI({ baseURL: `${base}/v1`, apiKey: key, maxRetries: 0 });
const claude = new Anthropic({ baseURL: base, apiKey: key, maxRetries: 0 });
const gemini = new GoogleGenAI({ apiKey: key, httpOptions: { baseUrl: base, apiVersion: 'v1beta' } });
for (const model of ['gpt-4o-mini', 'claude-3-5-sonnet', 'gemini-2.5-flash','llama3.1']) {
  const chat = { model, messages: [{ role: 'user', content: 'hello' }], max_tokens: 32 };
  assert.equal((await oai.chat.completions.create(chat)).choices[0].message.content, 'hello');
  assert.equal((await oai.chat.completions.stream(chat).finalChatCompletion()).choices[0].message.content, 'hello');
  assert.equal((await oai.responses.create({ model, input: 'hello', max_output_tokens: 32 })).output_text, 'hello');
  const response = await oai.responses.stream({ model, input: 'hello', max_output_tokens: 32 }).finalResponse();
  assert.equal(response.status, 'completed');
  assert.equal(response.output_text, 'hello');
  const message = { model, max_tokens: 32, messages: [{ role: 'user', content: 'hello' }] };
  assert.equal((await claude.messages.create(message)).content[0].text, 'hello');
  const final = await claude.messages.stream(message).finalMessage();
  assert.equal(final.content[0].text, 'hello');
  assert.equal(final.stop_reason, 'end_turn');
  const content = { model, contents: 'hello', config: { maxOutputTokens: 32 } };
  assert.equal((await gemini.models.generateContent(content)).text, 'hello');
  const chunks = [];
  for await (const chunk of await gemini.models.generateContentStream(content)) chunks.push(chunk);
  assert.equal(chunks.map(chunk => chunk.text ?? '').join(''), 'hello');
  assert.ok(chunks.some(chunk => chunk.candidates?.some(candidate => candidate.finishReason)));
}
const schema = {type:'object',properties:{value:{type:'string'}},required:['value']};
for (const model of ['gpt-4o-mini','claude-3-5-sonnet','gemini-2.5-flash','llama3.1']) {
  let tools = [{type:'function',function:{name:'echo',parameters:schema}}];
  const chat = await oai.chat.completions.stream({model,messages:[{role:'user',content:'use_tool'}],tools,max_tokens:32}).finalChatCompletion();
  const call = chat.choices[0].message.tool_calls[0];
  assert.equal(call.function.name,'echo');
  assert.equal(JSON.parse(call.function.arguments).value,'example');
  const chatReply = await oai.chat.completions.create({model,messages:[{role:'user',content:'use_tool'},chat.choices[0].message,{role:'tool',tool_call_id:call.id,content:'done'}],tools,max_tokens:32});
  assert.equal(chatReply.choices[0].message.content,'hello');
  tools = [{type:'function',name:'echo',parameters:schema}];
  const response = await oai.responses.stream({model,input:'use_tool',tools,max_output_tokens:32}).finalResponse();
  const responseCall = response.output[0];
  assert.equal(responseCall.name,'echo');
  assert.equal(JSON.parse(responseCall.arguments).value,'example');
  assert.equal((await oai.responses.create({model,input:[{role:'user',content:'use_tool'},responseCall,{type:'function_call_output',call_id:responseCall.call_id,output:'done'}],tools,max_output_tokens:32})).output_text,'hello');
  tools = [{name:'echo',input_schema:schema}];
  const message = await claude.messages.stream({model,messages:[{role:'user',content:'use_tool'}],tools,max_tokens:32}).finalMessage();
  const messageCall = message.content[0];
  assert.equal(messageCall.name,'echo');
  assert.equal(messageCall.input.value,'example');
  assert.equal((await claude.messages.create({model,messages:[{role:'user',content:'use_tool'},{role:'assistant',content:message.content},{role:'user',content:[{type:'tool_result',tool_use_id:messageCall.id,content:'done'}]}],tools,max_tokens:32})).content[0].text,'hello');
  const config = {maxOutputTokens:32,tools:[{functionDeclarations:[{name:'echo',parameters:schema}]}]};
  let geminiCall;
  for await (const chunk of await gemini.models.generateContentStream({model,contents:'use_tool',config})) {
    for (const call of chunk.functionCalls ?? []) { if (call.args?.value) geminiCall = call; }
  }
  assert.equal(geminiCall.name,'echo');
  assert.equal(geminiCall.args.value,'example');
  const reply = await gemini.models.generateContent({model,config,contents:[{role:'user',parts:[{text:'use_tool'}]},{role:'model',parts:[{functionCall:geminiCall}]},{role:'user',parts:[{functionResponse:{id:geminiCall.id,name:geminiCall.name,response:{result:'done'}}}]}]});
  assert.equal(reply.text,'hello');
}
const retried = await claude.messages.stream({model:'gpt-4o-mini',max_tokens:32,messages:[{role:'user',content:'retry_prelude'}]}).finalMessage();
assert.equal(retried.content[0].text,'hello');
assert.equal(retried.stop_reason,'end_turn');
const stored = await oai.responses.create({model:'gpt-4o-mini',input:'hello',max_output_tokens:32});
assert.equal((await oai.responses.create({model:'gpt-4o-mini',previous_response_id:stored.id,input:'continue',max_output_tokens:32})).output_text,'hello');
const foreign = new OpenAI({baseURL:`${base}/v1`,apiKey:process.env.GATEWAY_SDK_FOREIGN_KEY,maxRetries:0});
await assert.rejects(foreign.responses.create({model:'gpt-4o-mini',previous_response_id:stored.id,input:'continue',max_output_tokens:32}), {status:400});
await assert.rejects(oai.responses.create({model:'gpt-4o-mini',previous_response_id:'resp_unknown',input:'continue',max_output_tokens:32}), {status:400});
for (const model of ['gpt-4o-mini','claude-3-5-sonnet','gemini-2.5-flash','llama3.1']) {
  const success = async () => assert.equal((await oai.chat.completions.create({model,messages:[{role:'user',content:'hello'}],max_tokens:32})).choices[0].message.content,'hello');
  const streamFailure = error => error.status === undefined || error.status === 502;
  await success();
  await assert.rejects(oai.chat.completions.stream({model,messages:[{role:'user',content:'interrupt'}],max_tokens:32}).finalChatCompletion(),streamFailure);
  await success();
  let responseFailed = false;
  try {
    const final = await oai.responses.stream({model,input:'interrupt',max_output_tokens:32}).finalResponse();
    responseFailed = ['failed','incomplete'].includes(final.status);
  } catch (error) {
    assert.ok(streamFailure(error));
    responseFailed = true;
  }
  assert.ok(responseFailed);
  await success();
  await assert.rejects(claude.messages.stream({model,messages:[{role:'user',content:'interrupt'}],max_tokens:32}).finalMessage(),streamFailure);
  await success();
  await assert.rejects(async () => {
    for await (const chunk of await gemini.models.generateContentStream({model,contents:'interrupt',config:{maxOutputTokens:32}})) {
      assert.ok(chunk);
    }
  });
  await success();
}
console.log('TypeScript SDK: 32 text paths, 16 tool round trips, 16 interrupted streams and native state passed');
