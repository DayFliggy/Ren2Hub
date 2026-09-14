import os

from anthropic import Anthropic
from google import genai
from google.genai import types
from openai import OpenAI


base = os.environ["GATEWAY_SDK_URL"]
key = os.environ["GATEWAY_SDK_KEY"]
models = ["gpt-4o-mini", "claude-3-5-sonnet", "gemini-2.5-flash","llama3.1"]
oai = OpenAI(base_url=base + "/v1", api_key=key, max_retries=0)
claude = Anthropic(base_url=base, api_key=key, max_retries=0)
gemini = genai.Client(api_key=key, http_options=types.HttpOptions(base_url=base, api_version="v1beta"))

for model in models:
    response = oai.chat.completions.create(model=model, messages=[{"role": "user", "content": "hello"}], max_tokens=32)
    assert response.choices[0].message.content == "hello", (model, response)
    with oai.chat.completions.stream(model=model, messages=[{"role": "user", "content": "hello"}], max_tokens=32) as stream:
        for event in stream:
            pass
        final = stream.get_final_completion()
        assert final.choices[0].message.content == "hello", (model, final)

    response = oai.responses.create(model=model, input="hello", max_output_tokens=32)
    assert response.output_text == "hello", (model, response)
    with oai.responses.stream(model=model, input="hello", max_output_tokens=32) as stream:
        for event in stream:
            pass
        final = stream.get_final_response()
        assert final.status == "completed" and final.output_text == "hello", (model, final)

    response = claude.messages.create(model=model, max_tokens=32, messages=[{"role": "user", "content": "hello"}])
    assert response.content[0].text == "hello", (model, response)
    with claude.messages.stream(model=model, max_tokens=32, messages=[{"role": "user", "content": "hello"}]) as stream:
        final = stream.get_final_message()
        assert final.content[0].text == "hello" and final.stop_reason == "end_turn", (model, final)

    response = gemini.models.generate_content(model=model, contents="hello", config=types.GenerateContentConfig(max_output_tokens=32))
    assert response.text == "hello", (model, response)
    chunks = list(gemini.models.generate_content_stream(model=model, contents="hello", config=types.GenerateContentConfig(max_output_tokens=32)))
    assert "".join(chunk.text or "" for chunk in chunks) == "hello", (model, chunks)
    assert any(candidate.finish_reason for chunk in chunks for candidate in (chunk.candidates or [])), model

schema = {"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}
for model in models:
    tools = [{"type":"function","function":{"name":"echo","parameters":schema}}]
    with oai.chat.completions.stream(model=model,messages=[{"role":"user","content":"use_tool"}],tools=tools,max_tokens=32) as stream:
        final = stream.get_final_completion()
    call = final.choices[0].message.tool_calls[0]
    assert call.function.name == "echo" and 'example' in call.function.arguments
    response = oai.chat.completions.create(model=model,messages=[{"role":"user","content":"use_tool"},final.choices[0].message,{"role":"tool","tool_call_id":call.id,"content":"done"}],tools=tools,max_tokens=32)
    assert response.choices[0].message.content == "hello"
    tools = [{"type":"function","name":"echo","parameters":schema}]
    with oai.responses.stream(model=model,input="use_tool",tools=tools,max_output_tokens=32) as stream:
        final = stream.get_final_response()
    call = final.output[0]
    assert call.type == "function_call" and call.name == "echo" and 'example' in call.arguments
    response = oai.responses.create(model=model,input=[{"role":"user","content":"use_tool"},call,{"type":"function_call_output","call_id":call.call_id,"output":"done"}],tools=tools,max_output_tokens=32)
    assert response.output_text == "hello"
    tools = [{"name":"echo","input_schema":schema}]
    with claude.messages.stream(model=model,messages=[{"role":"user","content":"use_tool"}],tools=tools,max_tokens=32) as stream:
        final = stream.get_final_message()
    call = final.content[0]
    assert call.type == "tool_use" and call.name == "echo" and call.input["value"] == "example"
    response = claude.messages.create(model=model,messages=[{"role":"user","content":"use_tool"},{"role":"assistant","content":final.content},{"role":"user","content":[{"type":"tool_result","tool_use_id":call.id,"content":"done"}]}],tools=tools,max_tokens=32)
    assert response.content[0].text == "hello"
    config = types.GenerateContentConfig(max_output_tokens=32,tools=[types.Tool(function_declarations=[types.FunctionDeclaration(name="echo",parameters=schema)])],automatic_function_calling=types.AutomaticFunctionCallingConfig(disable=True))
    chunks = list(gemini.models.generate_content_stream(model=model,contents="use_tool",config=config))
    calls = [call for chunk in chunks for call in (chunk.function_calls or [])]
    call = next((call for call in reversed(calls) if call.args and "value" in call.args), calls[-1])
    assert call.name == "echo" and call.args.get("value") == "example", (model, calls)
    response = gemini.models.generate_content(model=model,contents=[types.Content(role="user",parts=[types.Part(text="use_tool")]),types.Content(role="model",parts=[types.Part(function_call=call)]),types.Content(role="user",parts=[types.Part(function_response=types.FunctionResponse(id=call.id,name=call.name,response={"result":"done"}))])],config=config)
    assert response.text == "hello"

with claude.messages.stream(model="gpt-4o-mini", max_tokens=32, messages=[{"role":"user","content":"retry_prelude"}]) as stream:
    final = stream.get_final_message()
    assert final.content[0].text == "hello" and final.stop_reason == "end_turn"

response = oai.responses.create(model="gpt-4o-mini", input="hello", max_output_tokens=32)
continued = oai.responses.create(model="gpt-4o-mini", previous_response_id=response.id, input="continue", max_output_tokens=32)
assert continued.output_text == "hello"
foreign = OpenAI(base_url=base + "/v1", api_key=os.environ["GATEWAY_SDK_FOREIGN_KEY"], max_retries=0)
try:
    foreign.responses.create(model="gpt-4o-mini", previous_response_id=response.id, input="continue", max_output_tokens=32)
except Exception as error:
    assert getattr(error, "status_code", None) == 400, error
else:
    raise AssertionError("another Token's response state was accepted")
try:
    oai.responses.create(model="gpt-4o-mini", previous_response_id="resp_unknown", input="continue", max_output_tokens=32)
except Exception as error:
    assert getattr(error, "status_code", None) == 400, error
else:
    raise AssertionError("unknown response state was accepted")

for model in models:
    for label, call in [
        ("chat", lambda: oai.chat.completions.stream(model=model, messages=[{"role":"user","content":"interrupt"}], max_tokens=32)),
        ("responses", lambda: oai.responses.stream(model=model, input="interrupt", max_output_tokens=32)),
        ("anthropic", lambda: claude.messages.stream(model=model, messages=[{"role":"user","content":"interrupt"}], max_tokens=32)),
    ]:
        assert oai.chat.completions.create(model=model, messages=[{"role":"user","content":"hello"}], max_tokens=32).choices[0].message.content == "hello"
        failed = False
        try:
            with call() as stream:
                for event in stream:
                    if label == "responses":
                        assert event.type != "response.completed", (model, event)
                        if event.type in ("response.failed", "response.incomplete"):
                            failed = True
        except AssertionError:
            raise
        except Exception as error:
            assert getattr(error, "status_code", None) in (None, 200, 500, 502), error
            if getattr(error, "status_code", None) in (200, 500):
                assert "stream_interrupted" in str(error), error
            failed = True
        assert failed, (model, label + " interrupted stream did not raise an SDK error")
    assert oai.chat.completions.create(model=model, messages=[{"role":"user","content":"hello"}], max_tokens=32).choices[0].message.content == "hello"
    failed = False
    try:
        list(gemini.models.generate_content_stream(model=model, contents="interrupt", config=types.GenerateContentConfig(max_output_tokens=32)))
    except AssertionError:
        raise
    except Exception as error:
        assert getattr(error, "code", None) in (None, 502), error
        failed = True
    assert failed, (model, "Gemini interrupted stream did not raise an SDK error")
    assert oai.chat.completions.create(model=model, messages=[{"role":"user","content":"hello"}], max_tokens=32).choices[0].message.content == "hello"

print("Python SDK: 32 text paths, 16 tool round trips, 16 interrupted streams and native state passed")
