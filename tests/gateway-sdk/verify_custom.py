import json
import os

from anthropic import Anthropic
from google import genai
from google.genai import types
from openai import OpenAI


base = os.environ["GATEWAY_SDK_URL"]
key = os.environ["GATEWAY_SDK_KEY"]
cases = json.loads(os.environ["GATEWAY_CUSTOM_CASES"])
oai = OpenAI(base_url=base + "/v1", api_key=key, max_retries=0)
claude = Anthropic(base_url=base, api_key=key, max_retries=0)
gemini = genai.Client(api_key=key, http_options=types.HttpOptions(base_url=base, api_version="v1beta"))
failures = []
successes = rejected = 0
for case in cases:
    for mode in ("json", "stream"):
        protocol, model = case["incoming"], case["model"]
        label = f"custom-{protocol}-{case['outgoing']}-{mode}"
        try:
            if protocol == "chat":
                request = {"model": model, "messages": [{"role": "user", "content": label}], "max_tokens": 32}
                if mode == "stream":
                    with oai.chat.completions.stream(**request) as stream:
                        response = stream.get_final_completion()
                else:
                    response = oai.chat.completions.create(**request)
                assert response.choices[0].message.content == "hello"
                assert response.choices[0].finish_reason == "stop"
                assert response.usage.prompt_tokens == 10 and response.usage.completion_tokens == 2
            elif protocol == "responses":
                request = {"model": model, "input": label, "max_output_tokens": 32}
                if mode == "stream":
                    with oai.responses.stream(**request) as stream:
                        response = stream.get_final_response()
                else:
                    response = oai.responses.create(**request)
                assert response.output_text == "hello" and response.status == "completed"
                assert response.usage.input_tokens == 10 and response.usage.output_tokens == 2
            elif protocol == "anthropic":
                request = {"model": model, "messages": [{"role": "user", "content": label}], "max_tokens": 32}
                if mode == "stream":
                    with claude.messages.stream(**request) as stream:
                        response = stream.get_final_message()
                else:
                    response = claude.messages.create(**request)
                assert response.content[0].text == "hello" and response.stop_reason == "end_turn"
                assert response.usage.input_tokens == 10 and response.usage.output_tokens == 2
            else:
                request = {"model": model, "contents": label, "config": types.GenerateContentConfig(max_output_tokens=32)}
                if mode == "stream":
                    chunks = list(gemini.models.generate_content_stream(**request))
                    assert "".join(chunk.text or "" for chunk in chunks) == "hello"
                    response = chunks[-1]
                else:
                    response = gemini.models.generate_content(**request)
                    assert response.text == "hello"
                assert response.candidates[0].finish_reason == "STOP"
                assert response.usage_metadata.prompt_token_count == 10 and response.usage_metadata.candidates_token_count == 2
            assert case["supported"], "unconfigured protocol was accepted"
            successes += 1
            print(f"CUSTOM_PATH_OK: {label}")
        except Exception as error:
            status = getattr(error, "status_code", None) or getattr(error, "code", None)
            if not case["supported"] and status == 503:
                rejected += 1
                print(f"CUSTOM_PATH_REJECTED: {label}")
            else:
                failures.append(f"{label}: {type(error).__name__}: {error}")
oai.close()
claude.close()
gemini.close()
assert not failures, "\n".join(failures)
assert successes == 22 and rejected == 10, (successes, rejected)
print("CUSTOM_SDK_OK: 22 text paths and 10 pre-send rejections (Python)")
