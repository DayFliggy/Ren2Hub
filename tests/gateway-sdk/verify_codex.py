import os
import urllib.request

from openai import OpenAI


base = os.environ["GATEWAY_SDK_URL"]
key = os.environ["GATEWAY_SDK_KEY"]
oai = OpenAI(base_url=base + "/v1", api_key=key, max_retries=0)
foreign = OpenAI(base_url=base + "/v1", api_key=os.environ["GATEWAY_SDK_FOREIGN_KEY"], max_retries=0)
response_defaults = {"store": False, "instructions": "standard OpenAI Codex", "max_output_tokens": 64}


def expect_status(action, expected):
    try:
        action()
    except Exception as error:
        status = getattr(error, "status_code", None) or getattr(error, "code", None)
        assert status == expected, (type(error).__name__, status, expected, str(error))
    else:
        raise AssertionError(f"expected HTTP {expected}")


def control(action):
    request = urllib.request.Request(os.environ["GATEWAY_CODEX_CONTROL"] + "/" + action, method="POST")
    with urllib.request.urlopen(request, timeout=5) as response:
        assert response.status == 204


def stream_response(**kwargs):
    with oai.responses.stream(**response_defaults, **kwargs) as stream:
        endings = [event.type for event in stream if event.type == "response.completed"]
        response = stream.get_final_response()
    assert endings == ["response.completed"] and response.status == "completed"
    assert response.output_text == "hello" and response.usage.output_tokens == 2
    return response


stored = oai.responses.create(**response_defaults, model="codex-fixture", input="codex-basic-json")
assert stored.id == "resp_codex-basic-json" and stored.output_text == "hello" and stored.usage.input_tokens == 10
stream_response(model="codex-fixture", input="codex-basic-stream")

for streaming in (False, True):
    options = {"model": "codex-fixture", "messages": [{"role": "user", "content": "codex-chat"}], "max_completion_tokens": 64}
    if streaming:
        with oai.chat.completions.stream(**options) as stream:
            completion = stream.get_final_completion()
    else:
        completion = oai.chat.completions.create(**options)
    assert completion.choices[0].message.content == "hello"
    assert completion.choices[0].finish_reason == "stop"

expect_status(lambda: oai.responses.create(**response_defaults, model="codex-fixture", previous_response_id="unknown", input="unknown-state"), 400)
expect_status(lambda: foreign.responses.create(**response_defaults, model="codex-fixture", previous_response_id=stored.id, input="foreign-state"), 400)
control("reorder")
assert oai.responses.create(**response_defaults, model="codex-fixture", previous_response_id=stored.id, input="codex-continue-json").output_text == "hello"
stream_response(model="codex-fixture", previous_response_id=stored.id, input="codex-continue-stream")
for action in ("disable", "replace"):
    control(action)
    expect_status(lambda: oai.responses.create(**response_defaults, model="codex-fixture", previous_response_id=stored.id, input="unavailable-state"), 503)

expect_status(lambda: oai.responses.create(**response_defaults, model="codex-errors", input="codex-error-rejected"), 400)
delivered, completed, failed = [], False, False
try:
    with oai.responses.stream(**response_defaults, model="codex-errors", input="codex-error-interrupt") as stream:
        for event in stream:
            if event.type == "response.output_text.delta":
                delivered.append(event.delta)
            completed = completed or event.type == "response.completed"
            failed = failed or event.type in ("response.failed", "response.incomplete", "error")
        failed = failed or stream.get_final_response().status in ("failed", "incomplete")
except Exception:
    failed = True
assert "".join(delivered) == "hello" and failed and not completed, (delivered, failed, completed)
print("CODEX_SDK_OK: 2 standard OpenAI Responses paths, 2 Chat paths, 2 owner rejections, 2 reordered continuations, 2 unavailable owners, 1 provider error, 1 delivered interruption (Python)")
