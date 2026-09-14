import os
import ssl

from openai import OpenAI
from websockets.exceptions import ConnectionClosedError


scenario = os.environ["GATEWAY_REALTIME_SCENARIO"]
client = OpenAI(base_url=os.environ["GATEWAY_SDK_URL"] + "/v1", api_key=os.environ["GATEWAY_SDK_KEY"], max_retries=0)
events = []
transport_error = False
with client.realtime.connect(
    model="gpt-4o-mini",
    websocket_connection_options={"ssl": ssl.create_default_context(cafile=os.environ["GATEWAY_SDK_CA"]), "proxy": None},
) as connection:
    try:
        for event in connection:
            events.append(event)
            if event.type == "session.created":
                connection.session.update(session={
                    "type": "realtime", "output_modalities": ["text"], "instructions": "isolated realtime sdk",
                    "tools": [{"type": "function", "name": "square", "parameters": {"type": "object", "properties": {"value": {"type": "integer"}}}}],
                })
                connection.conversation.item.create(item={"type": "message", "role": "user", "content": [{"type": "input_text", "text": "hello"}]})
                for identity in ("a", "b"):
                    connection.response.create(response={"conversation": "none", "metadata": {"identity": identity}})
            elif scenario == "client_close" and event.type == "response.output_text.delta":
                connection.close()
                break
            elif event.type == "response.done" and event.response.id == "response-b":
                tool = event.response.output[0]
                assert tool.call_id == "call-b" and tool.name == "square" and tool.arguments == '{"value":7}'
                connection.conversation.item.create(item={"type": "function_call_output", "call_id": tool.call_id, "output": "49"})
    except ConnectionClosedError:
        transport_error = True

assert events[0].type == "session.created"
assert any(event.type == "session.updated" and event.session.instructions == "isolated realtime sdk" for event in events)
text = [event for event in events if event.type == "response.output_text.delta"]
assert len(text) == 1 and text[0].response_id == "response-a" and text[0].delta == "hello"
completed = [event.response for event in events if event.type == "response.done"]
if scenario == "interleaved_completed":
    assert not transport_error, "completed Realtime session must close normally"
    assert [response.id for response in completed] == ["response-a", "response-b"]
    assert completed[0].output[0].content[0].text == "hello"
    assert [response.usage.total_tokens for response in completed] == [30, 12]
    arguments = [event for event in events if event.type == "response.function_call_arguments.delta"]
    assert [(event.response_id, event.delta) for event in arguments] == [("response-b", '{"value":'), ("response-b", "7}")]
    final_arguments = next(event for event in events if event.type == "response.function_call_arguments.done")
    assert final_arguments.arguments == '{"value":7}' and final_arguments.call_id == "call-b"
elif scenario == "interleaved_interrupted":
    assert [response.id for response in completed] == ["response-a"]
    assert transport_error
elif scenario == "provider_error":
    failure = next(event for event in events if event.type == "error")
    assert failure.error.code == "upstream_failure" and failure.error.message == "isolated upstream failure"
    assert not completed
else:
    assert not completed and not transport_error
client.close()
print(f"REALTIME_SDK_OK: {scenario} (Python typed event iterator)")
