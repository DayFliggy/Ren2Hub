import json
import os

from anthropic import Anthropic
from google import genai
from google.genai import types
from openai import OpenAI


base = os.environ["GATEWAY_SDK_URL"]
key = os.environ["GATEWAY_SDK_KEY"]
provider = os.environ["GATEWAY_CLOUD_PROVIDER"]
phase = os.environ.get("GATEWAY_CLOUD_PHASE", "text")
oai = OpenAI(base_url=base + "/v1", api_key=key, max_retries=0)
claude = Anthropic(base_url=base, api_key=key, max_retries=0)
gemini = genai.Client(api_key=key, http_options=types.HttpOptions(base_url=base, api_version="v1beta"))
failures = []

def assert_call_id(value, required=True):
    if provider == "vertex_gemini":
        assert not required or value
    else:
        assert value == "call_sdk"

for protocol in (("chat", "responses", "anthropic", "gemini") if phase == "text" else ()):
    for mode in ("json", "stream"):
        prompt = f"cloud-{protocol}-{mode}"
        try:
            if protocol == "chat":
                request = {"model": "gpt-4o-mini", "messages": [{"role": "user", "content": prompt}], "max_tokens": 32, "temperature": 0}
                if mode == "stream":
                    with oai.chat.completions.stream(**request) as stream:
                        response = stream.get_final_completion()
                else:
                    response = oai.chat.completions.create(**request)
                assert response.choices[0].message.content == "hello"
                assert response.choices[0].finish_reason == "stop"
                assert response.usage.prompt_tokens == 10 and response.usage.completion_tokens == 2
            elif protocol == "responses":
                request = {"model": "gpt-4o-mini", "input": prompt, "max_output_tokens": 32, "temperature": 0}
                if mode == "stream":
                    with oai.responses.stream(**request) as stream:
                        response = stream.get_final_response()
                else:
                    response = oai.responses.create(**request)
                assert response.output_text == "hello" and response.status == "completed"
                assert response.usage.input_tokens == 10 and response.usage.output_tokens == 2
            elif protocol == "anthropic":
                request = {"model": "gpt-4o-mini", "messages": [{"role": "user", "content": prompt}], "max_tokens": 32, "extra_body": {"temperature": 0}}
                if mode == "stream":
                    with claude.messages.stream(**request) as stream:
                        response = stream.get_final_message()
                else:
                    response = claude.messages.create(**request)
                assert response.content[0].text == "hello" and response.stop_reason == "end_turn"
                assert response.usage.input_tokens == 10 and response.usage.output_tokens == 2
            else:
                request = {"model": "gpt-4o-mini", "contents": prompt, "config": types.GenerateContentConfig(max_output_tokens=32, temperature=0, automatic_function_calling=types.AutomaticFunctionCallingConfig(disable=True))}
                if mode == "stream":
                    chunks = list(gemini.models.generate_content_stream(**request))
                    assert "".join(chunk.text or "" for chunk in chunks) == "hello"
                    response = chunks[-1]
                else:
                    response = gemini.models.generate_content(**request)
                    assert response.text == "hello"
                assert response.candidates[0].finish_reason == "STOP"
                assert response.usage_metadata.prompt_token_count == 10 and response.usage_metadata.candidates_token_count == 2
            print(f"CLOUD_PATH_OK: {provider}/{protocol}/{mode}")
        except Exception as error:
            failures.append(f"{provider}/{protocol}/{mode}: {type(error).__name__}: {error}")

schema = {"type": "object", "properties": {"value": {"type": "string"}}, "required": ["value"]}
for protocol in (("chat", "responses", "anthropic", "gemini") if phase == "tools_and_interrupts" else ()):
    try:
        if protocol == "chat":
            tools = [{"type": "function", "function": {"name": "echo", "parameters": schema}}]
            options = {"model": "gpt-4o-mini", "tools": tools, "max_tokens": 32, "temperature": 0}
            with oai.chat.completions.stream(messages=[{"role": "user", "content": "use_tool"}], **options) as stream:
                final = stream.get_final_completion()
            call = final.choices[0].message.tool_calls[0]
            assert_call_id(call.id)
            assert call.function.name == "echo" and json.loads(call.function.arguments) == {"value": "example"}
            response = oai.chat.completions.create(messages=[{"role": "user", "content": "use_tool"}, final.choices[0].message,
                {"role": "tool", "tool_call_id": call.id, "content": json.dumps({"result": "done"})}], **options)
            assert response.choices[0].message.content == "hello"
        elif protocol == "responses":
            tools = [{"type": "function", "name": "echo", "parameters": schema}]
            options = {"model": "gpt-4o-mini", "tools": tools, "max_output_tokens": 32, "temperature": 0}
            with oai.responses.stream(input="use_tool", **options) as stream:
                final = stream.get_final_response()
            call = final.output[0]
            assert_call_id(call.call_id)
            assert call.type == "function_call" and call.name == "echo" and json.loads(call.arguments) == {"value": "example"}
            response = oai.responses.create(input=[{"role": "user", "content": "use_tool"}, call,
                {"type": "function_call_output", "call_id": call.call_id, "output": json.dumps({"result": "done"})}], **options)
            assert response.output_text == "hello"
        elif protocol == "anthropic":
            options = {"model": "gpt-4o-mini", "tools": [{"name": "echo", "input_schema": schema}], "max_tokens": 32, "extra_body": {"temperature": 0}}
            with claude.messages.stream(messages=[{"role": "user", "content": "use_tool"}], **options) as stream:
                final = stream.get_final_message()
            call = final.content[0]
            assert_call_id(call.id)
            assert call.type == "tool_use" and call.name == "echo" and call.input == {"value": "example"}
            response = claude.messages.create(messages=[{"role": "user", "content": "use_tool"}, {"role": "assistant", "content": final.content},
                {"role": "user", "content": [{"type": "tool_result", "tool_use_id": call.id, "content": json.dumps({"result": "done"})}]}], **options)
            assert response.content[0].text == "hello"
        else:
            config = types.GenerateContentConfig(max_output_tokens=32, temperature=0,
                tools=[types.Tool(function_declarations=[types.FunctionDeclaration(name="echo", parameters=schema)])],
                automatic_function_calling=types.AutomaticFunctionCallingConfig(disable=True))
            chunks = list(gemini.models.generate_content_stream(model="gpt-4o-mini", contents="use_tool", config=config))
            calls = [call for chunk in chunks for call in (chunk.function_calls or [])]
            call = next(call for call in reversed(calls) if call.args and "value" in call.args)
            assert_call_id(call.id, required=False)
            assert call.name == "echo" and call.args == {"value": "example"}
            response = gemini.models.generate_content(model="gpt-4o-mini", contents=[types.Content(role="user", parts=[types.Part(text="use_tool")]),
                types.Content(role="model", parts=[types.Part(function_call=call)]),
                types.Content(role="user", parts=[types.Part(function_response=types.FunctionResponse(id=call.id, name=call.name, response={"result": "done"}))])], config=config)
            assert response.text == "hello"
        print(f"CLOUD_TOOL_OK: {provider}/{protocol}")
    except Exception as error:
        failures.append(f"{provider}/{protocol}/tool: {type(error).__name__}: {error}")

parallel_values = {"echo_zero": 0, "echo_false": False}
parallel_functions = [{"name": name, "parameters": {"type": "object", "properties": {"value": {"type": kind}}, "required": ["value"]}}
    for name, kind in (("echo_zero", "integer"), ("echo_false", "boolean"))]

def assert_parallel_calls(calls, native_gemini=False):
    assert len(calls) == 2, f"expected two calls, got {calls!r}"
    assert {name for _, name, _ in calls} == set(parallel_values)
    if provider != "vertex_gemini" or not native_gemini:
        assert len({identifier for identifier, _, _ in calls}) == 2 and all(identifier for identifier, _, _ in calls)
    for identifier, name, args in calls:
        expected = parallel_values[name]
        assert args == {"value": expected} and type(args["value"]) is type(expected)
        if provider != "vertex_gemini":
            assert identifier == name.replace("echo_", "call_")

for protocol in (("chat", "responses", "anthropic", "gemini") if phase == "parallel_tools" else ()):
    try:
        if protocol == "chat":
            options = {"model": "gpt-4o-mini", "tools": [{"type": "function", "function": function} for function in parallel_functions], "max_tokens": 32, "temperature": 0}
            with oai.chat.completions.stream(messages=[{"role": "user", "content": "use_parallel_tools"}], **options) as stream:
                endings = [choice.finish_reason for event in stream if event.type == "chunk" for choice in event.chunk.choices if choice.finish_reason]
                final = stream.get_final_completion()
            assert endings == ["tool_calls"] and final.usage.completion_tokens == 4
            calls = final.choices[0].message.tool_calls
            assert_parallel_calls([(call.id, call.function.name, json.loads(call.function.arguments)) for call in calls])
            results = [{"role": "tool", "tool_call_id": call.id, "content": json.dumps({"result": parallel_values[call.function.name]})} for call in reversed(calls)]
            response = oai.chat.completions.create(messages=[{"role": "user", "content": "use_parallel_tools"}, final.choices[0].message, *results], **options)
            assert response.choices[0].message.content == "hello"
        elif protocol == "responses":
            options = {"model": "gpt-4o-mini", "tools": [{"type": "function", **function} for function in parallel_functions], "max_output_tokens": 32, "temperature": 0}
            with oai.responses.stream(input="use_parallel_tools", **options) as stream:
                endings = [event.type for event in stream if event.type == "response.completed"]
                final = stream.get_final_response()
            assert endings == ["response.completed"] and final.status == "completed" and final.usage.output_tokens == 4
            calls = [item for item in final.output if item.type == "function_call"]
            assert_parallel_calls([(call.call_id, call.name, json.loads(call.arguments)) for call in calls])
            results = [{"type": "function_call_output", "call_id": call.call_id, "output": json.dumps({"result": parallel_values[call.name]})} for call in reversed(calls)]
            response = oai.responses.create(input=[{"role": "user", "content": "use_parallel_tools"}, *calls, *results], **options)
            assert response.output_text == "hello"
        elif protocol == "anthropic":
            options = {"model": "gpt-4o-mini", "tools": [{"name": function["name"], "input_schema": function["parameters"]} for function in parallel_functions], "max_tokens": 32, "extra_body": {"temperature": 0}}
            with claude.messages.stream(messages=[{"role": "user", "content": "use_parallel_tools"}], **options) as stream:
                endings = [event.type for event in stream if event.type == "message_stop"]
                final = stream.get_final_message()
            assert endings == ["message_stop"] and final.stop_reason == "tool_use" and final.usage.output_tokens == 4
            calls = [block for block in final.content if block.type == "tool_use"]
            assert_parallel_calls([(call.id, call.name, call.input) for call in calls])
            results = [{"type": "tool_result", "tool_use_id": call.id, "content": json.dumps({"result": parallel_values[call.name]})} for call in reversed(calls)]
            response = claude.messages.create(messages=[{"role": "user", "content": "use_parallel_tools"}, {"role": "assistant", "content": final.content}, {"role": "user", "content": results}], **options)
            assert response.content[0].text == "hello"
        else:
            config = types.GenerateContentConfig(max_output_tokens=32, temperature=0, tools=[types.Tool(function_declarations=parallel_functions)],
                automatic_function_calling=types.AutomaticFunctionCallingConfig(disable=True))
            chunks = list(gemini.models.generate_content_stream(model="gpt-4o-mini", contents="use_parallel_tools", config=config))
            calls = [call for chunk in chunks for call in (chunk.function_calls or [])]
            assert_parallel_calls([(call.id, call.name, call.args) for call in calls], native_gemini=True)
            assert chunks[-1].usage_metadata.candidates_token_count == 4
            results = [types.Part(function_response=types.FunctionResponse(id=call.id, name=call.name, response={"result": parallel_values[call.name]})) for call in reversed(calls)]
            response = gemini.models.generate_content(model="gpt-4o-mini", contents=[types.Content(role="user", parts=[types.Part(text="use_parallel_tools")]),
                types.Content(role="model", parts=[types.Part(function_call=call) for call in calls]), types.Content(role="user", parts=results)], config=config)
            assert response.text == "hello"
        print(f"CLOUD_PARALLEL_TOOL_OK: {provider}/{protocol}, two calls, arguments 0/false, reversed results")
    except Exception as error:
        failures.append(f"{provider}/{protocol}/parallel tools: {type(error).__name__}: {error}")

for protocol in (("chat", "responses", "anthropic", "gemini") if phase == "tools_and_interrupts" else ()):
    delivered = []
    failed = False
    try:
        if protocol == "chat":
            with oai.chat.completions.stream(model="gpt-4o-mini", messages=[{"role": "user", "content": "interrupt"}], max_tokens=32, temperature=0) as stream:
                for event in stream:
                    if event.type == "content.delta":
                        delivered.append(event.delta)
                    if event.type == "chunk":
                        assert not any(choice.finish_reason for choice in event.chunk.choices), "truncated completion emitted a finish reason"
                stream.get_final_completion()
        elif protocol == "responses":
            with oai.responses.stream(model="gpt-4o-mini", input="interrupt", max_output_tokens=32, temperature=0) as stream:
                for event in stream:
                    if event.type == "response.output_text.delta":
                        delivered.append(event.delta)
                    assert event.type != "response.completed", "truncated response emitted a success terminal"
                    failed = failed or event.type in ("response.failed", "response.incomplete")
                failed = failed or stream.get_final_response().status in ("failed", "incomplete")
        elif protocol == "anthropic":
            with claude.messages.stream(model="gpt-4o-mini", messages=[{"role": "user", "content": "interrupt"}], max_tokens=32, extra_body={"temperature": 0}) as stream:
                for event in stream:
                    if event.type == "content_block_delta" and event.delta.type == "text_delta":
                        delivered.append(event.delta.text)
                    assert event.type != "message_stop", "truncated message emitted a success terminal"
                stream.get_final_message()
        else:
            config = types.GenerateContentConfig(max_output_tokens=32, temperature=0, automatic_function_calling=types.AutomaticFunctionCallingConfig(disable=True))
            for chunk in gemini.models.generate_content_stream(model="gpt-4o-mini", contents="interrupt", config=config):
                delivered.append(chunk.text or "")
                assert not any(candidate.finish_reason for candidate in (chunk.candidates or []))
    except AssertionError as error:
        failures.append(f"{provider}/{protocol}/interruption terminal: {error}")
    except Exception:
        failed = True
    if not failed or "".join(delivered) != "hello":
        failures.append(f"{provider}/{protocol}/interruption: failed={failed}, delivered={delivered!r}")
    else:
        print(f"CLOUD_INTERRUPTION_OK: {provider}/{protocol}, delivered=hello")
    try:
        response = oai.chat.completions.create(model="gpt-4o-mini", messages=[{"role": "user", "content": "health_recovery"}], max_tokens=32, temperature=0)
        assert response.choices[0].message.content == "hello"
    except Exception as error:
        failures.append(f"{provider}/{protocol}/health recovery: {error}")
oai.close()
claude.close()
gemini.close()
assert not failures, "\n".join(failures)
summary = {"text": "8 text paths", "tools_and_interrupts": "4 tool round trips, 4 delivered interruptions", "parallel_tools": "4 parallel tool round trips"}
print(f"CLOUD_SDK_OK: {provider}, {summary[phase]} (Python)")
