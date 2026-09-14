import os
import urllib.request

from anthropic import Anthropic
from google import genai
from google.genai import types
from openai import OpenAI


base = os.environ["GATEWAY_SDK_URL"]
key = os.environ["GATEWAY_SDK_KEY"]
oai = OpenAI(base_url=base + "/v1", api_key=key, max_retries=0)
foreign = OpenAI(base_url=base + "/v1", api_key=os.environ["GATEWAY_SDK_FOREIGN_KEY"], max_retries=0)
claude = Anthropic(base_url=base, api_key=key, max_retries=0)
gemini = genai.Client(api_key=key, http_options=types.HttpOptions(base_url=base, api_version="v1beta", retry_options=types.HttpRetryOptions(attempts=1)))


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
    with oai.responses.stream(**kwargs) as stream:
        endings = [event.type for event in stream if event.type == "response.completed"]
        response = stream.get_final_response()
    assert endings == ["response.completed"] and response.status == "completed"
    assert response.output_text == "hello" and response.usage.output_tokens == 2
    return response


stored = oai.responses.create(model="codex-fixture", input="codex-basic-json")
assert stored.id == "resp_codex-basic-json" and stored.output_text == "hello" and stored.usage.input_tokens == 10
stream_response(model="codex-fixture", input="codex-basic-stream")

for streaming in (False, True):
    def chat():
        if streaming:
            with oai.chat.completions.stream(model="codex-fixture", messages=[{"role": "user", "content": "rejected-chat"}]) as stream:
                stream.get_final_completion()
        else:
            oai.chat.completions.create(model="codex-fixture", messages=[{"role": "user", "content": "rejected-chat"}])

    def anthropic():
        options = {"model": "codex-fixture", "messages": [{"role": "user", "content": "rejected-anthropic"}], "max_tokens": 32}
        if streaming:
            with claude.messages.stream(**options) as stream:
                stream.get_final_message()
        else:
            claude.messages.create(**options)

    def google():
        config = types.GenerateContentConfig(automatic_function_calling=types.AutomaticFunctionCallingConfig(disable=True))
        if streaming:
            list(gemini.models.generate_content_stream(model="codex-fixture", contents="rejected-gemini", config=config))
        else:
            gemini.models.generate_content(model="codex-fixture", contents="rejected-gemini", config=config)

    for action in (chat, anthropic, google):
        expect_status(action, 503)

expect_status(lambda: oai.responses.create(model="codex-fixture", previous_response_id="unknown", input="unknown-state"), 400)
expect_status(lambda: foreign.responses.create(model="codex-fixture", previous_response_id=stored.id, input="foreign-state"), 400)
control("reorder")
assert oai.responses.create(model="codex-fixture", previous_response_id=stored.id, input="codex-continue-json").output_text == "hello"
stream_response(model="codex-fixture", previous_response_id=stored.id, input="codex-continue-stream")
for action in ("disable", "replace"):
    control(action)
    expect_status(lambda: oai.responses.create(model="codex-fixture", previous_response_id=stored.id, input="unavailable-state"), 503)

expect_status(lambda: oai.responses.create(model="codex-errors", input="codex-error-rejected"), 400)
delivered, completed, failed = [], False, False
try:
    with oai.responses.stream(model="codex-errors", input="codex-error-interrupt") as stream:
        for event in stream:
            if event.type == "response.output_text.delta":
                delivered.append(event.delta)
            completed = completed or event.type == "response.completed"
            failed = failed or event.type in ("response.failed", "response.incomplete", "error")
        failed = failed or stream.get_final_response().status in ("failed", "incomplete")
except Exception:
    failed = True
assert "".join(delivered) == "hello" and failed and not completed, (delivered, failed, completed)
print("CODEX_SDK_OK: 2 Responses paths, 6 protocol rejections, 2 owner rejections, 2 reordered continuations, 2 unavailable owners, 1 provider error, 1 delivered interruption (Python)")
